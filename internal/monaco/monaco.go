// Package monaco persists Monaco editor settings for the ocode desktop app.
// Data lives under the global opencode data dir in a `monaco/` subdirectory:
//
//	~/.local/share/opencode/monaco/
//	  settings.json     — editor appearance & behaviour
//	  extensions.json   — enabled/disabled language services
package monaco

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/u007/ocode/internal/paths"
)

// DefaultTheme is the Monaco theme used at first launch.
const DefaultTheme = "ocode-dark"

// Settings holds all configurable Monaco editor options.
type Settings struct {
	Theme     string `json:"theme"`      // Monaco theme ID
	FontSize  int    `json:"font_size"`  // Editor font size in px
	TabSize   int    `json:"tab_size"`   // Spaces per tab
	WordWrap  bool   `json:"word_wrap"`  // Enable word wrapping
	Minimap   bool   `json:"minimap"`    // Show minimap
	LineNumbers bool `json:"line_numbers"`
}

// DefaultSettings returns a fresh Settings with sensible defaults.
func DefaultSettings() Settings {
	return Settings{
		Theme:       DefaultTheme,
		FontSize:    13,
		TabSize:     2,
		WordWrap:    true,
		Minimap:     true,
		LineNumbers: true,
	}
}

// Extension descibes a toggleable Monaco language service.
type Extension struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Enabled     bool   `json:"enabled"`
	Builtin     bool   `json:"builtin"` // true = Monaco built-in, false = custom
}

// BuiltinExtensions returns the default set of Monaco language services.
func BuiltinExtensions() []Extension {
	return []Extension{
		{Name: "typescript", Label: "TypeScript / JavaScript", Enabled: true, Builtin: true},
		{Name: "css", Label: "CSS / LESS / SCSS", Enabled: true, Builtin: true},
		{Name: "html", Label: "HTML", Enabled: true, Builtin: true},
		{Name: "json", Label: "JSON", Enabled: true, Builtin: true},
		{Name: "markdown", Label: "Markdown", Enabled: true, Builtin: true},
		{Name: "yaml", Label: "YAML", Enabled: true, Builtin: true},
		{Name: "python", Label: "Python", Enabled: true, Builtin: true},
		{Name: "go", Label: "Go", Enabled: true, Builtin: true},
		{Name: "rust", Label: "Rust", Enabled: true, Builtin: true},
		{Name: "cpp", Label: "C / C++", Enabled: true, Builtin: true},
		{Name: "java", Label: "Java", Enabled: true, Builtin: true},
		{Name: "shell", Label: "Shell", Enabled: true, Builtin: true},
		{Name: "sql", Label: "SQL", Enabled: true, Builtin: true},
	}
}

// Store manages Monaco settings on disk.
type Store struct {
	mu      sync.Mutex
	dir     string
	cfgPath string
	extPath string
}

// NewStore creates (or loads) the Monaco config store under the global data dir.
func NewStore() (*Store, error) {
	base, err := paths.GlobalDataDir()
	if err != nil {
		return nil, fmt.Errorf("monaco: resolve data dir: %w", err)
	}
	dir := filepath.Join(base, "monaco")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("monaco: mkdir %s: %w", dir, err)
	}
	s := &Store{
		dir:     dir,
		cfgPath: filepath.Join(dir, "settings.json"),
		extPath: filepath.Join(dir, "extensions.json"),
	}
	return s, nil
}

// --- Settings ---

// LoadSettings reads persisted settings, falling back to defaults on first run.
// If the file does not exist it is created with default values so that the
// on-disk state is always initialized.
func (s *Store) LoadSettings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()

	def := DefaultSettings()
	data, err := os.ReadFile(s.cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Seed the file with defaults so it exists from the start.
			if writeErr := s.writeSettingsLocked(def); writeErr != nil {
				log.Printf("monaco: seed settings: %v", writeErr)
			}
		}
		return def
	}
	var out Settings
	if err := json.Unmarshal(data, &out); err != nil {
		log.Printf("monaco: corrupt settings.json: %v (using defaults)", err)
		return def
	}
	// Fill zero values from defaults
	if out.Theme == "" {
		out.Theme = def.Theme
	}
	if out.FontSize <= 0 {
		out.FontSize = def.FontSize
	}
	if out.TabSize <= 0 {
		out.TabSize = def.TabSize
	}
	return out
}

// writeSettingsLocked writes settings to disk. Caller must hold s.mu.
func (s *Store) writeSettingsLocked(settings Settings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.cfgPath), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(s.cfgPath, data, 0644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	return nil
}

// SaveSettings persists editor settings.
func (s *Store) SaveSettings(settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeSettingsLocked(settings)
}

// --- Extensions ---

// LoadExtensions reads the persisted extension states, seeding defaults if
// the file does not exist yet. If the file is missing it is created with
// the built-in extension defaults so the on-disk state is always initialized.
func (s *Store) LoadExtensions() []Extension {
	s.mu.Lock()
	defer s.mu.Unlock()

	defs := BuiltinExtensions()
	data, err := os.ReadFile(s.extPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Seed the file with defaults so it exists from the start.
			if writeErr := s.writeExtensionsLocked(defs); writeErr != nil {
				log.Printf("monaco: seed extensions: %v", writeErr)
			}
		}
		return defs
	}
	var stored []Extension
	if err := json.Unmarshal(data, &stored); err != nil {
		log.Printf("monaco: corrupt extensions.json: %v (using defaults)", err)
		return defs
	}
	// Merge: stored values override defaults; unknown extensions keep defaults.
	byName := make(map[string]Extension, len(defs))
	for _, e := range stored {
		byName[e.Name] = e
	}
	out := make([]Extension, len(defs))
	for i, def := range defs {
		if s, ok := byName[def.Name]; ok {
			def.Enabled = s.Enabled
		}
		out[i] = def
	}
	return out
}

// writeExtensionsLocked writes extensions to disk. Caller must hold s.mu.
func (s *Store) writeExtensionsLocked(exts []Extension) error {
	data, err := json.MarshalIndent(exts, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal extensions: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.extPath), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(s.extPath, data, 0644); err != nil {
		return fmt.Errorf("write extensions: %w", err)
	}
	return nil
}

// SaveExtensions persists the extension enable states.
func (s *Store) SaveExtensions(exts []Extension) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeExtensionsLocked(exts)
}

// ToggleExtension flips the enabled state for a single extension by name.
func (s *Store) ToggleExtension(name string) error {
	exts := s.LoadExtensions()
	for i := range exts {
		if exts[i].Name == name {
			exts[i].Enabled = !exts[i].Enabled
			return s.SaveExtensions(exts)
		}
	}
	return fmt.Errorf("monaco: unknown extension %q", name)
}
