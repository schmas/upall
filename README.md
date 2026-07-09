# upall

Update every installed toolchain from one place, with a live master/detail TUI.

`upall` runs each tool's updater in order — OS packages, chezmoi, Homebrew, mise,
rust, uv, Claude CLI, ClaudeKit, fisher, atuin — capturing real colored output
through a pty and streaming it into a Bubble Tea dashboard. Steps are **config
driven**: nothing is hardcoded in Go. The current set ships as embedded default
plugins; you extend or override them with TOML in `$XDG_CONFIG_HOME/upall/steps.d/`.

> Status: under construction (Go rewrite of the v2 bash `upall`). See the plan in
> the dotfiles repo for phase-by-phase progress.

## Build

```sh
go build ./...
```

## Layout

- `internal/engine` — UI-agnostic step runner: pty capture, per-step timeout
  watchdog, log teeing, platform-agnostic run directories.
- `internal/platform` — host OS/distro/arch detection.
- `internal/notify` — best-effort desktop failure notification.
