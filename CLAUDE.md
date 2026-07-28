# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`upall` is a CLI/TUI that updates every installed toolchain (OS packages, Homebrew, mise, rust, uv, Claude CLI, AgentKit, fisher, atuin, chezmoi) from one place, streaming each updater's real output through a pty into a lazygit-style three-pane Bubble Tea dashboard. Personal dev-machine tool, config-driven — steps are TOML, not hardcoded Go.

**Further reading:** `README.md` (install, usage, TUI keybindings).

## Tech Stack

- **Language:** Go 1.26.5 (pinned in `mise.toml`, must match `go.mod`)
- **TUI:** charmbracelet/bubbletea + bubbles + lipgloss (Bubble Tea Elm architecture)
- **PTY:** creack/pty for capturing real colored output from step commands
- **Config:** BurntSushi/toml, embedded defaults + user overrides in `$XDG_CONFIG_HOME/upall/steps.d/`
- **Release:** goreleaser + svu (conventional-commit semver), automated on merge to `main`
- **Dev env:** mise (tool pinning) + direnv (`.envrc` scopes `GOBIN` to `.bin/` so `go install` doesn't shadow the released binary)

## Commands

```bash
# Build
go build ./cmd/upall

# Run locally
go run ./cmd/upall
go run ./cmd/upall --list          # list steps without running
go run ./cmd/upall brew mise       # run only named steps

# Tests (all packages)
go test ./...

# Tests (single package)
go test ./internal/tui/...

# Tests (single test)
go test ./internal/tui/... -run TestModel_Something -v

# Vet
go vet ./...

# Preview next release version (no side effects)
mise run release-preview
```

## Directory Structure

```
.
├── cmd/upall/              # main package, CLI arg parsing, sudo handling
├── internal/config/        # TOML step schema, embedded defaults, load/merge/resolve
│   └── defaults/           # one .toml per built-in step (brew, mise, rust, uv, ak, ...)
├── internal/engine/        # step runner: command exec, pty, logging, env, manifest
├── internal/history/       # read-only browser of past run logs/manifests
├── internal/notify/        # desktop notifications on run completion
├── internal/plain/         # non-TUI plain-output mode (--plain)
├── internal/platform/      # OS/platform detection
├── internal/settings/      # config.toml loading (keys, theme, history location)
└── internal/tui/           # Bubble Tea model/update/view, panes, pager, vt rendering
```

## Conventions

- Steps are defined as TOML (`schema = 1`, `[[step]]` with `key`, `label`, `detect`, `run`, `order`, `timeout`) — never hardcode a new tool's update logic in Go; add/override via `internal/config/defaults/*.toml` or user `steps.d/`.
- `GOBIN` is repo-scoped via `.envrc` (`$PWD/.bin`) — always `direnv allow` before `go install` in this repo to avoid shadowing the release binary on `PATH`.
- Bubble Tea code follows Model/Update/View split — `model.go` (state), `update.go` (Msg handling), `view.go` (rendering); pane-specific logic lives in dedicated files (`history_pane_test.go` pattern, `box.go`, `steps_filter`).
- Every step's full output is tee'd to `<history-dir>/<timestamp>/NN-key.log` plus a `manifest.json` — history/manifest formats are a public contract consumed by the History pane; don't change shape without updating both writer and reader.
- Releases are fully automated (svu + goreleaser on merge to `main`); there is no manual tag step.

## Rules

### Always

- Match `go.mod`'s `go` directive when bumping the Go version in `mise.toml` (CI installs via mise).
- Keep `internal/config/defaults/*.toml` step files self-contained and detectable via their `detect` command (steps must no-op cleanly on machines without the tool).
- Run `go test ./...` before considering engine/tui changes done — extensive existing test coverage (`*_test.go` in nearly every package) must stay green.
- Use conventional commits (`feat:`, `fix:`, `chore:`, etc.) — they directly drive the automated release version bump.

### Never

- Never hardcode a tool's update command directly in Go — express it as a step TOML.
- Never change the history log/manifest.json layout without updating both `internal/engine` (writer) and `internal/history`/`internal/tui` (reader).
- Never add a manual git tag/release step — the `release.yml` workflow on merge to `main` is the only release path.
