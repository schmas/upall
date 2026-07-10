# upall

Update every installed toolchain from one place, with a lazygit-style three-pane TUI.

`upall` runs each tool's updater in order — OS packages, chezmoi, Homebrew, mise,
rust, uv, Claude CLI, ClaudeKit, fisher, atuin — capturing real colored output
through a pty and streaming it into a Bubble Tea dashboard. Steps are **config
driven**: nothing is hardcoded in Go. The current set ships as embedded default
plugins; you extend or override them with TOML in `$XDG_CONFIG_HOME/upall/steps.d/`.

The dashboard has three titled, focus-highlighted panes — **Steps** (with
`All·Pending·Done` filter tabs and pre-run include/exclude), **History** (a
read-only browser of past runs), and **Output** — plus a config layer for keys,
theme, history location, and behavior (`$XDG_CONFIG_HOME/upall/config.toml`).

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
upall --init-config   Write a commented config.toml with all defaults.
upall --config-path   Print the resolved config file path.
upall --version       Print version.
upall -h | --help     Show this help.
```

`UPALL_KEEP=N` retains N run-log dirs (default 10; overrides `config.toml`). Every
step's full output is tee'd to `<history-dir>/<timestamp>/NN-key.log`, and each
run also writes a small `manifest.json` (status + durations) for the History pane.
The history dir defaults to `~/.cache/upall` and is set by `[history] dir`.

## Keys (TUI)

`Tab` / `Shift+Tab` cycle focus across the Steps → Output → History panes; the
focused pane's border is highlighted and `↑`/`↓` and clicks route to it. Every
key below is rebindable via `[keys]` in `config.toml`.

| Key | Action | Pane |
|-----|--------|------|
| `tab` / `shift+tab` | Cycle / reverse-cycle pane focus | any |
| `↑`/`k`, `↓`/`j` | Move the cursor in the focused pane | any |
| `←`/`→` (or `[`/`]`) | Cycle the `All·Pending·Done` filter | Steps |
| `space` | Include/exclude the selected step before a run | Steps (idle) |
| `⏎` (or `s`) | Start the run (idle) / follow the running step | Steps |
| `a` | Show all step logs concatenated | Steps |
| `r` | Retry the selected failed step (only when idle) | Steps |
| `R` | Re-run every included step | Steps |
| `g` / `G` | Scroll to top / bottom | Output |
| `⏎`/`→`, `←` | Expand-or-select / collapse a past run (or click) | History |
| `l` | Open the selected log in the pager | Steps / History |
| `c` / `C` | Open `config.toml` / the config folder | any |
| `?` | Toggle the full-key footer | any |
| `q` / `ctrl-c` | Quit (cancels a running step) | any |

The Steps filter tabs are **view-only** — they never change what runs.
Excluding a step with `space` (dimmed + struck through) skips it for the run; the
header `N/M` counts only included steps. The History pane is **read-only**:
expanding a run reveals its steps (each with its own duration) and an `All logs`
child that load past logs into the Output pane (in-pane, plus `l` for the full log
in the pager). Clicking a run header expands/collapses it; clicking a step opens
its log.

Plain streaming is used automatically for a non-TTY stdout, `--plain`, or `NO_COLOR`.

## Configuring keys, theme, and behavior

On first run `upall` seeds a fully-commented `config.toml` at
`$XDG_CONFIG_HOME/upall/config.toml` (fallback `~/.config/upall/`); `--init-config`
rewrites it on demand and `--config-path` prints its location. Every option is a
partial override — only the fields you set win; the rest stay at their defaults.
Precedence is **CLI flag › env › `config.toml` › built-in default**.

```toml
schema = 1

[keys]                       # rebind any action to a list of keys
quit = ["q", "ctrl+c"]

[theme]                      # named color, 256-index, or hex
accent  = "42"               # focused border, selected row, progress fill
dim     = "240"

[history]
dir  = "~/.cache/upall"      # single root: new runs are written and browsed here
keep = 10                    # run-log dirs to retain

[ui]
default_filter = "all"       # all | pending | done
wrap           = true        # wrap long lines in the Output pane
follow         = true        # follow the active step's output
wide_threshold = 90          # cols at/above which panes sit side by side
pager          = ""          # pager command; empty = $PAGER, then "less -R"

[notify]
enabled = true               # desktop notification on a failed run
```

Rebindable actions: `up, down, top, bottom, start, follow, all-logs, retry,
restart, pager, quit, focus-next, focus-prev, filter-next, filter-prev, toggle,
expand, collapse, open-config, open-config-dir`.

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

- `cmd/upall` — the command entrypoint (arg parsing, sudo priming, config load, TUI/plain wiring).
- `internal/engine` — UI-agnostic step runner: pty capture, per-step timeout
  watchdog, log teeing, run directories, per-run manifest.
- `internal/config` — TOML step schema, 2-layer merge, platform/detect resolution.
- `internal/settings` — user `config.toml`: keys, theme, history, UI, notify.
- `internal/history` — read-only scan of past run dirs + lazy log loading.
- `internal/plain` — plain streaming sink + shared summary (writes the manifest).
- `internal/tui` — Bubble Tea three-pane dashboard (Steps / History / Output).
- `internal/platform`, `internal/notify` — host detection, failure notification.

For architecture, code standards, testing strategy, and roadmap, see [`.ck-docs/`](.ck-docs/).
