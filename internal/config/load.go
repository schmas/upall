package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

// Load reads the embedded defaults and the user layer, then merges them
// field-wise by key (user overrides only the fields it sets). The returned defs
// are the effective step definitions, not yet resolved against the platform.
func Load() ([]StepDef, error) {
	embedded, err := loadEmbedded()
	if err != nil {
		return nil, err
	}
	user, err := loadUserLayer()
	if err != nil {
		return nil, err
	}
	return Merge(append(embedded, user...)), nil
}

// loadEmbedded decodes every defaults/*.toml, in filename order (a stable
// tiebreaker only; actual run order comes from each step's `order`).
func loadEmbedded() ([]StepDef, error) {
	names, err := fs.Glob(defaultsFS, "defaults/*.toml")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var out []StepDef
	for _, name := range names {
		data, err := defaultsFS.ReadFile(name)
		if err != nil {
			return nil, err
		}
		defs, err := parseFile(name, data)
		if err != nil {
			return nil, err
		}
		out = append(out, defs...)
	}
	return out, nil
}

// loadUserLayer reads $XDG_CONFIG_HOME/upall/steps.d/*.toml (falling back to
// ~/.config). A missing directory is not an error — it just means no overrides.
func loadUserLayer() ([]StepDef, error) {
	dir := UserStepsDir()
	if dir == "" {
		return nil, nil
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.toml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var out []StepDef
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		defs, err := parseFile(name, data)
		if err != nil {
			return nil, err
		}
		out = append(out, defs...)
	}
	return out, nil
}

// ConfigDir resolves upall's config base directory, honoring XDG_CONFIG_HOME
// with a ~/.config fallback (macOS usually has no XDG vars set). Both steps.d
// and config.toml resolve under it, so the XDG logic lives in one place.
// Returns "" only when the home directory cannot be resolved.
func ConfigDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "upall")
}

// UserStepsDir resolves the user override directory (ConfigDir()/steps.d).
func UserStepsDir() string {
	dir := ConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "steps.d")
}

// parseFile decodes one TOML file, tagging errors with the file name so a bad
// config reports "file: reason" instead of crashing.
func parseFile(name string, data []byte) ([]StepDef, error) {
	var f File
	if err := toml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if f.Schema != 1 {
		return nil, fmt.Errorf("%s: unsupported schema %d (want 1)", name, f.Schema)
	}
	for i, s := range f.Steps {
		if s.Key == "" {
			return nil, fmt.Errorf("%s: step %d has no key", name, i)
		}
	}
	return f.Steps, nil
}
