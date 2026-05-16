package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/snapshot"
)

type FormatTool struct {
	Config *config.Config
}

func (t FormatTool) Name() string        { return "format" }
func (t FormatTool) Description() string { return "Format a file using configured formatter" }
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
	var params struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if t.Config == nil || len(t.Config.Formatters) == 0 {
		return "No formatters configured", nil
	}

	safe, err := confinedPath(params.Path)
	if err != nil {
		return "", err
	}

	if err := FormatFile(safe, t.Config.Formatters); err != nil {
		return "", err
	}

	return fmt.Sprintf("Successfully formatted %s", params.Path), nil
}

func FormatFile(path string, formatters map[string]config.FormatterConfig) error {
	ext := strings.ToLower(filepath.Ext(path))
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
		if strings.TrimPrefix(ext, ".") == key {
			matched = &fmtCfg
			matchedKey = key
			break
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
		snapshot.Backup(path) //nolint:errcheck
		if err := os.WriteFile(path, formatted, 0644); err != nil {
			return fmt.Errorf("write formatted %s: %w", path, err)
		}
	}

	return nil
}

func FormatAfterWrite(path string, formatters map[string]config.FormatterConfig) {
	if len(formatters) == 0 {
		return
	}
	_ = FormatFile(path, formatters)
}
