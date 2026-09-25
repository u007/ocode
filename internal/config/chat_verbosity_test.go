package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultChatVerbosityConfig(t *testing.T) {
	got := defaultChatVerbosityConfig()
	if got.Preset != "full" {
		t.Fatalf("preset = %q, want full", got.Preset)
	}
	for name, value := range map[string]string{
		"older_thinking":   got.Overrides.OlderThinking,
		"tool_calls":       got.Overrides.ToolCalls,
		"tool_output":      got.Overrides.ToolOutput,
		"activity_notices": got.Overrides.ActivityNotices,
	} {
		if value != "preset" {
			t.Errorf("override %s = %q, want preset", name, value)
		}
	}
}

func TestChatVerbosityConfigValidateRejectsUnknownValues(t *testing.T) {
	tests := []struct {
		name string
		cfg  ChatVerbosityConfig
	}{
		{
			name: "preset",
			cfg:  ChatVerbosityConfig{Preset: "loud"},
		},
		{
			name: "older thinking override",
			cfg: ChatVerbosityConfig{
				Preset:    "full",
				Overrides: ChatVerbosityOverrides{OlderThinking: "sometimes"},
			},
		},
		{
			name: "tool output override",
			cfg: ChatVerbosityConfig{
				Preset:    "full",
				Overrides: ChatVerbosityOverrides{ToolOutput: "hidden"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); err == nil {
				t.Fatal("Validate() = nil, want error")
			}
		})
	}
}

func TestResolveChatVerbosityPolicy(t *testing.T) {
	tests := []struct {
		name              string
		cfg               ChatVerbosityConfig
		wantOlderThinking string
		wantToolCalls     string
		wantToolOutput    string
		wantNotices       string
	}{
		{
			name:              "full",
			cfg:               ChatVerbosityConfig{Preset: "full"},
			wantOlderThinking: "expanded",
			wantToolCalls:     "expanded",
			wantToolOutput:    "expanded",
			wantNotices:       "expanded",
		},
		{
			name:              "balanced",
			cfg:               ChatVerbosityConfig{Preset: "balanced"},
			wantOlderThinking: "collapsed",
			wantToolCalls:     "expanded",
			wantToolOutput:    "expanded",
			wantNotices:       "expanded",
		},
		{
			name:              "quiet",
			cfg:               ChatVerbosityConfig{Preset: "quiet"},
			wantOlderThinking: "collapsed",
			wantToolCalls:     "collapsed",
			wantToolOutput:    "collapsed",
			wantNotices:       "collapsed",
		},
		{
			name: "override",
			cfg: ChatVerbosityConfig{
				Preset: "balanced",
				Overrides: ChatVerbosityOverrides{
					ToolCalls:  "collapsed",
					ToolOutput: "expanded",
				},
			},
			wantOlderThinking: "collapsed",
			wantToolCalls:     "collapsed",
			wantToolOutput:    "expanded",
			wantNotices:       "expanded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveChatVerbosityPolicy(tt.cfg)
			if err != nil {
				t.Fatalf("ResolveChatVerbosityPolicy() error = %v", err)
			}
			if got.OlderThinking != tt.wantOlderThinking {
				t.Errorf("OlderThinking = %q, want %q", got.OlderThinking, tt.wantOlderThinking)
			}
			if got.LatestThinking != "expanded" {
				t.Errorf("LatestThinking = %q, want expanded", got.LatestThinking)
			}
			if got.ToolCalls != tt.wantToolCalls {
				t.Errorf("ToolCalls = %q, want %q", got.ToolCalls, tt.wantToolCalls)
			}
			if got.ToolOutput != tt.wantToolOutput {
				t.Errorf("ToolOutput = %q, want %q", got.ToolOutput, tt.wantToolOutput)
			}
			if got.Notices != tt.wantNotices {
				t.Errorf("Notices = %q, want %q", got.Notices, tt.wantNotices)
			}
			if got.Status != "expanded" {
				t.Errorf("Status = %q, want expanded", got.Status)
			}
		})
	}
}

func TestChatVerbosityConfigRoundTrip(t *testing.T) {
	chdirTempForConfigTest(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	seed := defaultOcodeConfig()
	seed.Editor = "vim"
	seed.ChatVerbosity = ChatVerbosityConfig{
		Preset: "quiet",
		Overrides: ChatVerbosityOverrides{
			OlderThinking:   "expanded",
			ToolCalls:       "collapsed",
			ToolOutput:      "collapsed",
			ActivityNotices: "collapsed",
		},
	}
	if err := SaveOcodeConfig(&seed); err != nil {
		t.Fatalf("SaveOcodeConfig() error = %v", err)
	}

	got, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy() error = %v", err)
	}
	if got.Editor != "vim" {
		t.Errorf("Editor = %q, want vim", got.Editor)
	}
	if got.ChatVerbosity.Preset != "quiet" || got.ChatVerbosity.Overrides.OlderThinking != "expanded" || got.ChatVerbosity.Overrides.ActivityNotices != "collapsed" {
		t.Errorf("ChatVerbosity = %+v, want persisted quiet policy", got.ChatVerbosity)
	}
}

func TestChatVerbosityConfigFileRejectsUnknownOverrideCategory(t *testing.T) {
	chdirTempForConfigTest(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dir := filepath.Join(tmp, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	body := `{"chat_verbosity":{"preset":"full","overrides":{"notices":"collapsed"}}}`
	if err := os.WriteFile(filepath.Join(dir, "ocodeconfig.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := LoadOcodeConfigCopy(); err == nil {
		t.Fatal("LoadOcodeConfigCopy() = nil, want unknown-category error")
	}
}

func TestSaveOcodeChatVerbosityWritesSpecCategoryKeys(t *testing.T) {
	chdirTempForConfigTest(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfg := defaultChatVerbosityConfig()
	cfg.Preset = "balanced"
	cfg.Overrides.OlderThinking = "collapsed"
	cfg.Overrides.ActivityNotices = "expanded"
	if err := SaveOcodeChatVerbosity(cfg); err != nil {
		t.Fatalf("SaveOcodeChatVerbosity() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tmp, ".config", "opencode", "ocodeconfig.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	for _, key := range []string{`"older_thinking"`, `"activity_notices"`} {
		if !strings.Contains(string(data), key) {
			t.Fatalf("persisted config missing %s: %s", key, data)
		}
	}
	if strings.Contains(string(data), `"thinking"`) || strings.Contains(string(data), `"notices"`) {
		t.Fatalf("persisted config still uses legacy keys: %s", data)
	}
}

func TestSaveOcodeChatVerbosityRejectsInvalidWithoutRewrite(t *testing.T) {
	chdirTempForConfigTest(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	seed := defaultOcodeConfig()
	seed.Editor = "nano"
	if err := SaveOcodeConfig(&seed); err != nil {
		t.Fatalf("seed SaveOcodeConfig() error = %v", err)
	}
	path := filepath.Join(tmp, ".config", "opencode", "ocodeconfig.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config before invalid save: %v", err)
	}

	err = SaveOcodeChatVerbosity(ChatVerbosityConfig{Preset: "unknown"})
	if err == nil {
		t.Fatal("SaveOcodeChatVerbosity() = nil, want validation error")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after invalid save: %v", err)
	}
	if string(after) != string(before) {
		t.Fatal("invalid save rewrote ocodeconfig.json")
	}
}
