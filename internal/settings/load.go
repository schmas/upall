package settings

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/schmas/upall/internal/config"
	"github.com/schmas/upall/internal/engine"
)

// fileConfig is the TOML decode form. Scalar fields are POINTERS so an unset
// field (nil, inherit the default) is distinct from an explicit zero value —
// the same trick internal/config uses for StepDef.
type fileConfig struct {
	Schema  int                 `toml:"schema"`
	Keys    map[string][]string `toml:"keys"`
	Theme   themeTOML           `toml:"theme"`
	History historyTOML         `toml:"history"`
	UI      uiTOML              `toml:"ui"`
	Notify  notifyTOML          `toml:"notify"`
	Run     runTOML             `toml:"run"`
}

type themeTOML struct {
	Accent  *string `toml:"accent"`
	Dim     *string `toml:"dim"`
	Success *string `toml:"success"`
	Failure *string `toml:"failure"`
}

type historyTOML struct {
	Dir  *string `toml:"dir"`
	Keep *int    `toml:"keep"`
}

type uiTOML struct {
	DefaultFilter *string `toml:"default_filter"`
	Wrap          *bool   `toml:"wrap"`
	Follow        *bool   `toml:"follow"`
	WideThreshold *int    `toml:"wide_threshold"`
	Pager         *string `toml:"pager"`
}

type notifyTOML struct {
	Enabled *bool `toml:"enabled"`
}

type runTOML struct {
	Shell *string `toml:"shell"`
}

// ConfigPath is the resolved config file path
// ($XDG_CONFIG_HOME/upall/config.toml, fallback ~/.config). Empty if the home
// directory cannot be resolved.
func ConfigPath() string {
	dir := config.ConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "config.toml")
}

// Load reads config.toml and merges it over Defaults(). A missing file returns
// Defaults() with no error; a malformed or unsupported file returns an error
// tagged with the file name.
func Load() (Settings, error) {
	path := ConfigPath()
	if path == "" {
		return Defaults(), nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Defaults(), nil
	}
	if err != nil {
		return Defaults(), fmt.Errorf("%s: %w", path, err)
	}
	return parse(filepath.Base(path), data)
}

// parse decodes one config file's bytes and merges the set fields over
// Defaults(). name tags any error with the file it came from.
func parse(name string, data []byte) (Settings, error) {
	var fc fileConfig
	md, err := toml.Decode(string(data), &fc)
	if err != nil {
		return Settings{}, fmt.Errorf("%s: %w", name, err)
	}
	if fc.Schema != 1 {
		return Settings{}, fmt.Errorf("%s: unsupported schema %d (want 1)", name, fc.Schema)
	}
	// Reject unknown keys (e.g. a typo like `[theme] accnt`) so a mistyped option
	// is a loud error, not a silently-ignored no-op. Keys under [keys] decode into
	// the action map and are validated separately below.
	if und := md.Undecoded(); len(und) > 0 {
		return Settings{}, fmt.Errorf("%s: unknown key %q", name, und[0].String())
	}

	s := Defaults()

	// [keys]: overlay each set action over the default bindings; reject unknown
	// action names so a typo is a loud error, not a silently-dead binding.
	for action, keys := range fc.Keys {
		if !isKnownAction(action) {
			return Settings{}, fmt.Errorf("%s: unknown [keys] action %q", name, action)
		}
		s.Keys[action] = keys
	}

	setStr(&s.Theme.Accent, fc.Theme.Accent)
	setStr(&s.Theme.Dim, fc.Theme.Dim)
	setStr(&s.Theme.Success, fc.Theme.Success)
	setStr(&s.Theme.Failure, fc.Theme.Failure)

	if fc.History.Dir != nil {
		s.History.Dir = expandHome(*fc.History.Dir)
	}
	setInt(&s.History.Keep, fc.History.Keep)

	setStr(&s.UI.DefaultFilter, fc.UI.DefaultFilter)
	setBool(&s.UI.Wrap, fc.UI.Wrap)
	setBool(&s.UI.Follow, fc.UI.Follow)
	setInt(&s.UI.WideThreshold, fc.UI.WideThreshold)
	setStr(&s.UI.Pager, fc.UI.Pager)

	setBool(&s.Notify.Enabled, fc.Notify.Enabled)

	setStr(&s.Run.Shell, fc.Run.Shell)

	return s, nil
}

func setStr(dst *string, v *string) {
	if v != nil {
		*dst = *v
	}
}

func setInt(dst *int, v *int) {
	if v != nil {
		*dst = *v
	}
}

func setBool(dst *bool, v *bool) {
	if v != nil {
		*dst = *v
	}
}

// expandHome expands a leading ~ (or ~/) to the user's home directory. Any
// other path is returned unchanged.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// ResolveKeep applies the run-retention precedence: UPALL_KEEP env ›
// config.toml keep › engine.DefaultKeep. configKeep of 0 means "unset".
func ResolveKeep(configKeep int) int {
	if v := os.Getenv("UPALL_KEEP"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	if configKeep > 0 {
		return configKeep
	}
	return engine.DefaultKeep
}
