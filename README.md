# upall

Update every installed toolchain from one place, with a lazygit-style three-pane TUI.

`upall` runs each tool's updater in order — OS packages, chezmoi, Homebrew, mise,
rust, uv, Claude CLI, AgentKit, fisher, atuin — capturing real colored output
through a pty and streaming it into a Bubble Tea dashboard. Steps are **config
driven**: nothing is hardcoded in Go. The current set ships as embedded default
plugins; you extend or override them with TOML in `$XDG_CONFIG_HOME/upall/steps.d/`.

The dashboard has three titled, focus-highlighted panes — **Steps** (with
`All·Pending·Done` filter tabs and pre-run include/exclude), **History** (a
read-only browser of past runs), and **Output** — plus a config layer for keys,
theme, history location, and behavior (`$XDG_CONFIG_HOME/upall/config.toml`).

`upall` also updates itself: it notices a newer GitHub release on launch and
`--update` (or `U` in the TUI) replaces the running binary in place after a
confirm prompt. See [Self-update](#self-update).

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

If you self-update with `upall --update`, run `chezmoi apply --refresh-externals`
afterwards. Within the 168h `refreshPeriod`, plain `chezmoi apply`/`chezmoi
update` reuses the cached archive and would restore the previous version over
the one you just installed.

### From source

```sh
go install github.com/schmas/upall/cmd/upall@latest
```

### Releasing (maintainer)

Releases are **automatic on merge to `main`**. The release workflow derives the
version from [conventional commits](https://www.conventionalcommits.org/) via
`svu` — `feat:` bumps minor, `fix:` bumps patch, `feat!:`/`BREAKING CHANGE:` bumps
major; a merge with only `chore:`/`docs:`/`ci:` commits publishes nothing. When a
bump is due it smoke-tests all four targets, then goreleaser publishes.

```sh
mise run release-preview   # what the next merge to main would release (no side effects)
```

## Usage

```
upall                 Run every applicable step, in order.
upall brew mise       Run only the named steps.
upall --list          List steps (key, label, applies?, detect-ok?).
upall --plain         Force plain output (no color); still tees logs.
upall --init-config   Write a commented config.toml with all defaults.
upall --config-path   Print the resolved config file path.
upall --check-update  Check for a newer release now (ignores the cache).
upall --update        Replace upall with the latest release, after a prompt.
upall --update --yes  Same, without the prompt (for non-interactive use).
upall --version       Print version.
upall -h | --help     Show this help.
```

### Self-update

`upall` checks GitHub for a newer release on launch, at most once per
`[update] check_interval` (default 16h), and mentions one in the TUI footer or
after the plain-mode summary. The check runs alongside your steps and never
delays them.

**A failed check never fails a run.** No network, a rate-limited API, or a bad
response prints a one-line reason and nothing else — the run's exit code is
unaffected. `--check-update` and `--update` exit `1` when they could not reach a
verdict, distinct from `2` for a usage error (e.g. `--update` with no terminal
and no `--yes`), so a script can tell "couldn't check" from "wrong invocation".

`--update` downloads the release archive for your platform, verifies its sha256
against the release's `checksums.txt`, and replaces the running binary with an
atomic rename. The previous binary is kept as `.upall.old` next to it until the
next launch, and the new one has to pass a `--version` smoke test or it is
rolled back — a failed update never leaves you without a working `upall`. Both
downloads must come from an allowlisted GitHub host over HTTPS. That proves the
bytes arrived intact from the published release; there is no code signing, so it
does not vouch for the release itself.

In the TUI the check shows up on the right of the footer: `v1.4.0 → v1.5.0` when
a newer release exists, or a quiet `v1.4.0 (check failed)` when the check could
not reach a verdict. Both persist until the next check replaces them — neither is
a toast, and neither ever opens a dialog over the dashboard. Hints keep priority
over the badge on a narrow terminal.

`U` first makes sure it knows what it's confirming: if no update is already known
this session — never checked yet, the cache said current, or the last check
failed — it runs a live check (ignoring the cache, same as `--check-update`) and
shows a `checking…` state in the footer while it does. Once it has an answer it
either opens the confirm prompt (a release exists) or reports what it found in
the footer (`already on v0.6.1`, or the failure reason) — retrying is exactly
pressing `U` again. If an update is already known, `U` skips straight to confirm
with no extra request.

Confirming (`⏎`/`y`) shows live download progress, and once the new binary is in
place it exits the dashboard the same way `q` does — summary, logs, and manifest
all written — before restarting in place with your original arguments. `U` is a
no-op while a run is active (the footer says to stop the run first), so a
self-update can never orphan a step's child process. Quitting mid-update
abandons the download but still waits for an install already underway, so
exiting can never leave you between two binaries; a failed install prints its
full reason (including any manual recovery step) after the dashboard closes.

Set `[update] enabled = false` to switch all of it off — the launch check, the
footer badge, `U`, and both flags.

`UPALL_KEEP=N` retains N run-log dirs (default 10; overrides `config.toml`). Every
step's full output is tee'd to `<history-dir>/<timestamp>/NN-key.log`, and each
run also writes a small `manifest.json` (status + durations) for the History pane.
The history dir defaults to `~/.cache/upall` and is set by `[history] dir`.

## Keys (TUI)

`Tab` / `Shift+Tab` cycle focus across the Steps → History → Output panes; the
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
| `u` | Resume: re-run the aborted step and every step after it (only when idle) | Steps |
| `R` | Re-run every included step | Steps |
| `g` / `G` | Scroll to top / bottom | Output |
| `⏎`/`→`, `←` | Expand-or-select / collapse a past run (or click) | History |
| `w` | Toggle line wrap for a history log in the Output pane | any |
| `l` | Open the selected log in the pager | Steps / History |
| `c` / `C` | Open `config.toml` / the config folder | any |
| `U` | Self-update: re-check if needed, then confirm, download, replace, and restart in place | any (when idle) |
| `?` | Open the Keybindings panel (`↑`/`↓` scroll, `/` search, `esc` close) | any |
| `x` | Stop the current run and stay in the TUI (no-op when idle) | any |
| `i` | Type mode: forward keystrokes to the running step (e.g. a sudo password) | any (during a run) |
| `esc` | Exit type mode back to normal navigation | Output (typing) |
| `q` / `ctrl-c` | Quit (cancels a running step) | any |

The Steps filter tabs are **view-only** — they never change what runs.
Excluding a step with `space` (dimmed + struck through) skips it for the run; the
header `N/M` counts only included steps. The History pane is **read-only**:
expanding a run reveals its steps (each with its own duration) and an `All logs`
child that load past logs into the Output pane (in-pane, plus `l` for the full log
in the pager). Moving the cursor with `↑`/`↓` loads the row's log into the Output
pane after a short pause, so skimming past runs never decodes every log; `⏎` and
clicks load immediately. Clicking a run header expands/collapses it; clicking a
step opens its log.

`?` opens a floating **Keybindings** panel over the layout, grouped into
sections and scrollable with `↑`/`↓`, `g`/`G`, PgUp/PgDn and the mouse wheel.
Every key it prints comes from your live bindings, so a `[keys]` rebind shows
through. `?` or `esc` closes it; `q`/`ctrl-c` still quit upall from inside it,
and `x` (stop) and `i` (type) keep working so an open panel can never hide or
block a running step's password prompt.

`/` inside the panel filters the list as you type, matching descriptions, the
keys as shown, and the underlying key names (searching `enter` finds the `⏎`
rows). In the prompt one `esc` clears the filter and leaves it, `⏎` leaves it
and keeps the filter, and every printable key is text — including `q`, so only
a non-printable quit chord (`ctrl-c` by default) still exits from there.

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

[run]
shell = "bash"               # default shell for steps without their own `shell`

[update]
enabled        = true        # false also disables --check-update / --update
check_interval = "16h"       # how long a version check is cached
```

The step shell resolves as `step.shell` › `[run] shell` (default `bash`) ›
built-in fallback (`bash` if present, else `sh`). The configured shell is used
only when it is on `PATH`, so the `bash` default still degrades to `sh` on a
host without bash; a per-step `shell` is used verbatim.

Rebindable actions: `up, down, top, bottom, start, follow, all-logs, retry,
continue, restart, pager, stop, type, quit, focus-next, focus-prev, filter-next,
filter-prev, toggle, expand, collapse, wrap, open-config, open-config-dir,
self-update, help`.

Action names are validated, but keys are not checked for uniqueness: binding an
action to a key an earlier-dispatched action already owns silently shadows it
(`help = ["x"]` stops the run instead of opening the panel). Some keys are
shared on purpose — `enter` drives start, follow and expand.

`self-update` (default `U`) checks for a newer release and, after a confirm
prompt, replaces the running binary in place. `[update] enabled = false`
switches it off along with the launch check and the footer badge.

`stop` (default `x`) cancels the active run and leaves the TUI open: the running
step is marked aborted, steps that had not started stay pending, and the header
goes idle. Unlike `quit` it does not exit, so `r` (retry), `u` (continue), and
`R` (re-run) still work afterwards. It is a no-op when no run is active.

Every step's stdin is now a live pty, not `/dev/null`, so a step can prompt for
input mid-run — the case that motivates this is a step whose `run` invokes
`sudo` and needs an interactive password (e.g. on a remote host without a
1Password/Touch-ID sudo integration): the prompt shows up in the Output pane
like any other output.

Keystrokes are **not** forwarded by default — they stay bound to the usual
shortcuts (e.g. `c`/`C` open the config), so typing a password without arming
type mode first would trigger those actions instead. When a running step's
output looks like a password prompt, upall detects it and shows a
`⚠ waiting for input · press i to type` warning in the Output title and a
`type password` hint in the footer. Press `type` (default `i`) — from any pane
during a run — to forward keystrokes straight to the step; type the password,
`⏎` to send it, `esc` to leave type mode. A step nobody types into just blocks
on that read — its `timeout` (or `stop`) is still the way out.

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
