package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/snapshot"
)

type FormatTool struct {
	Config *config.Config
}

func (t FormatTool) Name() string        { return "format" }
func (t FormatTool) Description() string { return "Format a file using configured formatter" }
func (t FormatTool) Parallel() bool      { return false }
func (t FormatTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "format",
		"description": "Format a file using configured formatter",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to format",
				},
			},
			"required": []string{"path"},
		},
	}
}

func (t FormatTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

func (t FormatTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	safe, err := confinedPath(ctx, params.Path)
	if err != nil {
		return "", err
	}

	var formatters map[string]config.FormatterConfig
	if t.Config != nil {
		formatters = t.Config.Formatters
	}

	if err := FormatFile(ctx, safe, formatters); err != nil {
		return "", err
	}

	return fmt.Sprintf("Successfully formatted %s", params.Path), nil
}

// builtinFormatter returns the zero-config default formatter for an
// extension when the user hasn't configured one, or nil if none exists.
// go uses the locally installed gofmt; ts/tsx/sql shell out to npx, which
// auto-installs prettier/sql-formatter on first use if not already cached.
func builtinFormatter(ext, baseName string) *config.FormatterConfig {
	switch ext {
	case "go":
		return &config.FormatterConfig{Command: "gofmt"}
	case "ts", "tsx":
		return &config.FormatterConfig{Command: "npx", Args: []string{"--yes", "prettier", "--stdin-filepath", baseName}}
	case "sql":
		return &config.FormatterConfig{Command: "npx", Args: []string{"--yes", "sql-formatter"}}
	default:
		return nil
	}
}

func FormatFile(ctx context.Context, path string, formatters map[string]config.FormatterConfig) error {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	baseName := filepath.Base(path)

	var matched *config.FormatterConfig
	var matchedKey string

	for key, fmtCfg := range formatters {
		if len(fmtCfg.Files) > 0 {
			for _, pattern := range fmtCfg.Files {
				fileMatched, err := filepath.Match(pattern, baseName)
				if err == nil && fileMatched {
					matched = &fmtCfg
					matchedKey = key
					break
				}
			}
		}
		if matched != nil {
			break
		}
		if ext == key {
			matched = &fmtCfg
			matchedKey = key
			break
		}
	}

	if matched == nil {
		if _, configured := formatters[ext]; !configured {
			if matched = builtinFormatter(ext, baseName); matched != nil {
				matchedKey = ext
			}
		}
	}

	if matched == nil || matched.Command == "" {
		return nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	cmd := exec.Command(matched.Command, matched.Args...)
	cmd.Stdin = bytes.NewReader(content)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("formatter %s failed: %s: %w", matchedKey, stderr.String(), err)
	}

	formatted := stdout.Bytes()
	if len(formatted) == 0 {
		return nil
	}

	if !bytes.Equal(content, formatted) {
		tcID := snapshot.ToolCallIDFromContext(ctx)
		_ = snapshot.FromContext(ctx).Backup(path, tcID) //nolint:errcheck
		if err := os.WriteFile(path, formatted, 0644); err != nil {
			return fmt.Errorf("write formatted %s: %w", path, err)
		}
	}

	return nil
}

func FormatAfterWrite(ctx context.Context, path string, formatters map[string]config.FormatterConfig) {
	if len(formatters) == 0 {
		return
	}
	_ = FormatFile(ctx, path, formatters)
}
