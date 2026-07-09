# upall

Update every installed toolchain from one place, with a live master/detail TUI.

`upall` runs each tool's updater in order — OS packages, chezmoi, Homebrew, mise,
rust, uv, Claude CLI, ClaudeKit, fisher, atuin — capturing real colored output
through a pty and streaming it into a Bubble Tea dashboard. Steps are **config
driven**: nothing is hardcoded in Go. The current set ships as embedded default
plugins; you extend or override them with TOML in `$XDG_CONFIG_HOME/upall/steps.d/`.

## Install

### Via chezmoi (auto-latest)

The dotfiles repo pulls the latest release binary through `.chezmoiexternal.toml`:

```toml
[".local/bin/upall"]
  type = "archive-file"
  url  = "https://github.com/schmas/upall/releases/latest/download/upall_{{ .chezmoi.os }}_{{ .chezmoi.arch }}.tar.gz"
  path = "upall"
  executable = true
  refreshPeriod = "168h"
```

`chezmoi apply --refresh-externals` pulls a newer release.

### From source

```sh
go install github.com/schmas/upall/cmd/upall@latest
```

## Usage

```
upall                 Run every applicable step, in order.
upall brew mise       Run only the named steps.
upall --list          List steps (key, label, applies?, detect-ok?).
upall --plain         Force plain output (no color); still tees logs.
upall --version       Print version.
upall -h | --help     Show this help.
```

`UPALL_KEEP=N` retains N run-log dirs under `~/.cache/upall` (default 10). Every
step's full output is tee'd to `~/.cache/upall/<timestamp>/NN-key.log`.

## Keys (TUI)

| Key | Action |
|-----|--------|
| `↑`/`k`, `↓`/`j` | Move selection |
| `⏎` | Follow the running step (autoscroll) |
| `a` | Show all logs concatenated |
| `r` | Retry the selected failed step (only when idle) |
| `l` | Open the selected step's log in `$PAGER` |
| `g` / `G` | Top / bottom |
| `q` / `ctrl-c` | Quit (cancels a running step) |

Plain streaming is used automatically for a non-TTY stdout, `--plain`, or `NO_COLOR`.

## Configuring steps

Each step is a TOML `[[step]]` with `schema = 1`. Drop files in
`$XDG_CONFIG_HOME/upall/steps.d/` (fallback `~/.config/upall/steps.d/`) to add new
steps or override embedded defaults. A user file with a matching `key` overrides
only the fields it sets; everything else is inherited.

```toml
schema = 1

[[step]]
key     = "brew"        # matches the embedded default → merges onto it
order   = 5             # only this field changes; run/sudo/detect inherited

[[step]]
key     = "mytool"      # a brand-new step
label   = "My Tool"
os      = ["linux"]     # platform predicate (empty = any)
detect  = "command -v mytool"   # run via `sh -c`; exit 0 => applies
run     = ["mytool update"]
shell   = "bash"        # default; use "fish" etc. as needed
sudo    = false
timeout = "20m"         # per-step hang watchdog
env     = { FOO = "bar" }
```

Fields: `key, label, os[], distro[], detect, shell, sudo, run[], env{}, enabled,
order, timeout`. Set `enabled = false` to disable an embedded default.

> Remote/downloadable plugins are intentionally **not** supported — steps come
> only from the embedded defaults and your local `steps.d/`. `detect`/`run` are
> shell-evaluated, which is safe only because every source is local and trusted.

## Layout

- `cmd/upall` — the command entrypoint (arg parsing, sudo priming, TUI/plain wiring).
- `internal/engine` — UI-agnostic step runner: pty capture, per-step timeout
  watchdog, log teeing, run directories.
- `internal/config` — TOML schema, 2-layer merge, platform/detect resolution.
- `internal/plain` — plain streaming sink + shared summary.
- `internal/tui` — Bubble Tea master/detail dashboard.
- `internal/platform`, `internal/notify` — host detection, failure notification.

For architecture, code standards, testing strategy, and roadmap, see [`.ck-docs/`](.ck-docs/).
