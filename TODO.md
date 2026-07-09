# upall — TODO

Backlog of planned enhancements. Not yet implemented.

## Run history browser (view logs from previous runs)

Add a **run-history pane** in the left sidebar (below the step list) that lists
previous runs and lets you open their logs.

The data already exists on disk — no new capture work needed:

- Runs are kept under `~/.cache/upall/` (or `$XDG_CACHE_HOME/upall/`), one
  timestamped dir per run (`20060102-150405`), rotated to the newest `UPALL_KEEP`
  (default 10). See `internal/engine/log.go` (`NewRunDir`, `rotate`) and
  `LogPath(runDir, pos, key)` for the per-step logfile path
  (`NN-<key>.log`). A `latest` symlink points at the most recent run.

Sketch:

- On start, scan the cache root for run dirs (reverse-chronological), parse the
  timestamp for a human label (e.g. "today 15:24", "2d ago").
- Sidebar gains a third section: **Steps · All logs · History**. Selecting a past
  run swaps the detail pane to that run's logs (read the `NN-*.log` files; reuse
  the ring/wrap/pager rendering path, or open directly in `$PAGER`).
- Read-only: history rows can't be retried/restarted (those act on the live run).
- Keep it lazy — only read a run's logfiles when it's selected, not upfront.

Open question: show history only when idle, or always? Leaning always-visible but
non-focusable while a run is in flight.

## lazygit-style boxes: colored borders + titled panes

Reprofile the TUI to look like lazygit (see
https://github.com/jesseduffield/lazygit for reference): each pane is a rounded
box with a **title in the border** ("Steps", "Logs", "History") and the
**focused pane's border is highlighted** (bright color) while others stay dim.

Notes / constraints:

- lazygit uses `jroimartin/gocui` + `gdamore/tcell`, **not** our stack
  (Bubble Tea + Lip Gloss). Use it as a *visual* reference only — do not port
  its widget code.
- Lip Gloss already supports what's needed: `Border(lipgloss.RoundedBorder())`
  with `BorderForeground(...)` per pane, and title-in-border via
  `Border...Foreground` + a rendered title row, or lipgloss v2's border-title
  helpers if we upgrade. Today only the master pane has a right rule
  (`masterStyle` in `internal/tui/view.go`); give each region (steps, logs,
  history) its own titled bordered box and drive border color off which pane is
  focused.
- Add pane focus state to the model (which box has keyboard focus) so borders can
  highlight and `↑/↓`/click route to the focused pane.

### Faithful output rendering (fixes residual "weird chars")

Some escape artifacts still leak through the line `sanitize()` pass
(`internal/tui/sanitize.go`) — acceptable for now, but the real fix is to render
step output through a **VT emulator** that keeps a screen grid instead of
stripping escapes. Use pure-Go `github.com/charmbracelet/x/vt` (same ecosystem,
`CGO_ENABLED=0`-friendly, has scrollback). **Not** `go.mitchellh.com/libghostty`
— it's cgo + a native Zig-built lib, which breaks the static cross-compiled
single-binary distribution. Pairs naturally with the box rework above.
