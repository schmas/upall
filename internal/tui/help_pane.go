package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/x/ansi"
)

const (
	helpTitle = "Keybindings"
	helpMinW  = 30  // preferred floor, never applied past the frame's own width
	helpMaxW  = 100 // absolute ceiling, so the list stays readable on a wide screen
	// helpWidthPct is the share of the frame the panel prefers to take, which is
	// what makes it read as a panel rather than a tooltip on a wide terminal.
	helpWidthPct = 70
	// helpMargin is the columns left uncovered on each side when the frame has
	// room, so the panes stay visible around the panel.
	helpMargin = 4
	helpGutter = 1 // blank column before the key
	helpKeyGap = 2 // gap between the key column and the descriptions
)

// helpRow is one listed binding. keys is what the panel prints (glyphs), while
// rawKeys keeps the tea key names behind it: the displayed "⏎" is unsearchable,
// so the filter matches against both. runnable marks the rows enter can execute
// — the literal rows describe hardcoded prompt keys with no action to run.
type helpRow struct {
	keys    string
	rawKeys []string
	desc    string
	// action is the name this binding has in [keys], which is the word a user
	// who has edited config.toml will type into the filter. Descriptions are
	// prose and cannot be relied on to contain it — "re-run the selected step"
	// does not contain "retry".
	action   string
	runnable bool
}

// helpLine is one rendered body line. row is the index of the binding it shows
// within the currently listed set, or -1 for a section rule or a spacer, which
// is what lets the cursor step over bindings while the viewport scrolls lines.
type helpLine struct {
	text string
	row  int
}

// helpSection is a labeled group of rows, drawn under a centered rule.
type helpSection struct {
	title string
	rows  []helpRow
}

// helpView is one laid-out panel: the lines to draw, the box to draw them in,
// and how many bindings are listed. shown counts BINDINGS, not body lines, so
// the "6 of 65" readout tracks the cursor over things you can select rather
// than over section rules and spacers.
type helpView struct {
	lines []helpLine
	rect  rect
	shown int
}

// keyGlyphs renders tea key names the way the footer already writes them.
// Anything absent passes through unchanged (ctrl+c, g, home, letters).
var keyGlyphs = map[string]string{
	"up":        "↑",
	"down":      "↓",
	"left":      "←",
	"right":     "→",
	"enter":     "⏎",
	"shift+tab": "⇧tab",
	" ":         "space",
}

// formatKeys renders a binding's keys as one caption, e.g. ["up","k"] → "↑/k".
func formatKeys(keys []string) string {
	out := make([]string, len(keys))
	for i, k := range keys {
		if g, ok := keyGlyphs[k]; ok {
			out[i] = g
		} else {
			out[i] = k
		}
	}
	return strings.Join(out, "/")
}

// helpSections groups every binding for the panel. Rows read their caption and
// keys straight off the binding, so a [keys] rebind shows through with no
// second table to keep in sync.
func (k keyMap) helpSections() []helpSection {
	// The action name is repeated here because a key.Binding does not carry it;
	// TestHelpSectionsUseKnownActions keeps every one of them a real action.
	row := func(action string, b key.Binding) helpRow {
		return helpRow{
			keys: b.Help().Key, rawKeys: b.Keys(), desc: b.Help().Desc,
			action: action, runnable: true,
		}
	}
	// Literal rows for keys that are genuinely hardcoded (not rebindable
	// actions) — the one sanctioned exception to deriving from the keymap.
	// They are not runnable: they describe what a key means inside a prompt
	// that is not open, so there is nothing for enter to trigger.
	lit := func(keys, desc string) helpRow {
		return helpRow{keys: keys, rawKeys: []string{keys}, desc: desc}
	}
	return []helpSection{
		{"Navigation", []helpRow{
			row("up", k.Up), row("down", k.Down), row("top", k.Top), row("bottom", k.Bottom),
		}},
		{"Panes & Focus", []helpRow{
			row("focus-next", k.FocusNext), row("focus-prev", k.FocusPrev),
		}},
		{"Steps", []helpRow{
			row("start", k.Start), row("follow", k.Follow), row("toggle", k.Toggle),
			row("filter-prev", k.FilterPrev), row("filter-next", k.FilterNext),
		}},
		{"Run Control", []helpRow{
			row("retry", k.Retry), row("continue", k.Continue), row("restart", k.Restart),
			row("stop", k.Stop), row("type", k.TypeMode),
		}},
		{"Output & Pager", []helpRow{
			row("all-logs", k.All), row("wrap", k.Wrap), row("pager", k.Pager),
		}},
		{"History", []helpRow{row("expand", k.Expand), row("collapse", k.Collapse)}},
		{"Config & Tools", []helpRow{
			row("open-config", k.OpenConfig), row("open-config-dir", k.OpenConfigDir),
			row("self-update", k.SelfUpdate),
		}},
		{"General", []helpRow{row("help", k.Help), row("quit", k.Quit)}},
		{"Prompts", []helpRow{
			lit("esc", "leave type mode · close this panel"),
			lit("y/n", "confirm or decline a self-update"),
			lit("/", "filter this list"),
		}},
	}
}

// helpLayout is the single sizing-and-layout pass: it returns the panel's body
// lines and the rectangle to draw them in. Geometry is derived from the frame
// it will be composited onto, never from m.width/m.height — the rendered frame
// is neither the terminal's size at small heights nor rectangular.
//
// Callers: helpOverlay (with the measured frame) and the scroll handlers (with
// m.width/m.height, close enough to bound an offset; the render clamps again
// and is authoritative).
func (m *Model) helpLayout(frameW, frameH int) helpView {
	all := m.keys.helpSections()

	// Every dimension is measured from the FULL list, so filtering changes what
	// the panel contains and never how big it is or where its key column sits.
	// A box that resized under the cursor while typing would be unreadable.
	keyW, contentW := helpMetrics(all)
	panelW := helpPanelWidth(contentW, frameW)
	panelH := min(helpBodyLen(all)+2, max(3, frameH-2))
	panelH = min(panelH, max(2, frameH))

	secs := filterSections(all, m.helpQuery)
	var lines []helpLine
	if len(secs) == 0 {
		lines = []helpLine{{text: " " + m.st.muted.Render(fmt.Sprintf("no match for %q", m.helpQuery)), row: -1}}
	} else {
		lines = helpBody(secs, keyW, m.st)
	}

	return helpView{
		lines: lines,
		shown: countRows(secs),
		rect: rect{
			x: max(0, (frameW-panelW)/2),
			y: max(0, (frameH-panelH)/2),
			w: panelW,
			h: panelH,
		},
	}
}

// helpMetrics measures the key column and the widest content line across the
// whole list.
func helpMetrics(secs []helpSection) (keyW, contentW int) {
	for _, s := range secs {
		for _, r := range s.rows {
			keyW = max(keyW, ansi.StringWidth(r.keys))
		}
	}
	for _, s := range secs {
		contentW = max(contentW, ansi.StringWidth(s.title)+8) // "─── " + title + " ───"
		for _, r := range s.rows {
			contentW = max(contentW, keyW+2+ansi.StringWidth(r.desc))
		}
	}
	return keyW, contentW
}

// helpPanelWidth sizes the box: at least as wide as its content, and otherwise
// a generous share of the frame so it reads as a panel rather than a tooltip.
// The cap always wins — clampInt is deliberately avoided here, since it raises
// hi to lo when the two invert, which on a 20-column terminal would hand back
// a 30-wide panel.
func helpPanelWidth(contentW, frameW int) int {
	preferred := max(contentW+4, frameW*helpWidthPct/100)
	limit := min(frameW, helpMaxW)
	if frameW >= helpMinW+2*helpMargin {
		limit = min(limit, frameW-2*helpMargin) // keep the panes visible around it
	}
	return max(min(max(preferred, min(helpMinW, frameW)), limit), 2)
}

// helpBodyLen is the line count helpBody would produce, without rendering it:
// one rule plus one line per row per section, and a spacer between sections.
func helpBodyLen(secs []helpSection) int {
	n := 0
	for i, s := range secs {
		if i > 0 {
			n++
		}
		n += 1 + len(s.rows)
	}
	return n
}

func countRows(secs []helpSection) int {
	n := 0
	for _, s := range secs {
		n += len(s.rows)
	}
	return n
}

// helpBody lays the sections out as inner box lines: a section rule indented to
// the description column, then one right-aligned key column plus description
// per row, with a blank spacer between sections. Rows carry their index in the
// listed set so the cursor can address them.
func helpBody(secs []helpSection, keyW int, st styles) []helpLine {
	descCol := strings.Repeat(" ", helpGutter+keyW+helpKeyGap)
	var body []helpLine
	row := 0
	for i, s := range secs {
		if i > 0 {
			body = append(body, helpLine{row: -1})
		}
		// Aligned to the descriptions rather than centered on the box: the eye
		// follows one left edge down the list instead of two.
		body = append(body, helpLine{
			text: descCol + st.selected.Render("─── "+s.title+" ───"),
			row:  -1,
		})
		for _, r := range s.rows {
			pad := max(0, keyW-ansi.StringWidth(r.keys))
			body = append(body, helpLine{
				text: strings.Repeat(" ", helpGutter+pad) + st.keycap.Render(r.keys) +
					strings.Repeat(" ", helpKeyGap) + st.muted.Render(r.desc),
				row: row,
			})
			row++
		}
	}
	return body
}

// lineOfRow finds the body line showing a given row index, or -1.
func lineOfRow(lines []helpLine, row int) int {
	for i, l := range lines {
		if l.row == row {
			return i
		}
	}
	return -1
}

// helpOverlay renders the panel over frame and returns the composited frame,
// unchanged in line count and per-line width (see overlay).
func (m *Model) helpOverlay(frame string) string {
	frameW, frameH := frameSize(frame)
	v := m.helpLayout(frameW, frameH)
	r := v.rect

	visible := max(0, r.h-2)
	// Clamped locally rather than written back: View must not mutate the model,
	// and the handlers keep the stored offset in range on their own.
	off := helpScrollTo(v, m.helpCursor, m.helpOffset, visible)
	// The min is required, not defensive: visible routinely exceeds the line
	// count (a one-line no-match body), and the panic would propagate out of
	// View and cancel a live run.
	end := min(off+visible, len(v.lines))

	rows := make([]string, 0, end-off)
	for _, l := range v.lines[off:end] {
		if l.row >= 0 && l.row == m.helpCursor {
			// Styled from stripped text, like the Steps and History cursors: the
			// keycap's own SGR reset would otherwise end the bar mid-row.
			rows = append(rows, m.st.cursor.Render(ansi.Strip(l.text)))
			continue
		}
		rows = append(rows, l.text)
	}

	box := titledBox(helpTitle, "", strings.Join(rows, "\n"), true, r.w, r.h, m.st,
		scrollbarThumb(visible, len(v.lines), visible, off))
	// The position readout goes on the bottom edge, where a list's count lives
	// in lazygit, leaving the top border to the title alone.
	box = bottomBorderLabel(box, m.helpCount(v), true, m.st)
	return overlay(frame, box, r.x, r.y)
}

// helpScrollTo returns the offset that keeps the cursor row on screen, starting
// from the stored offset so unrelated scrolling is preserved.
func helpScrollTo(v helpView, cursor, off, visible int) int {
	off = clampHelpOffset(off, len(v.lines), visible)
	if visible == 0 {
		return off
	}
	line := lineOfRow(v.lines, cursor)
	if line < 0 {
		return off
	}
	if line < off {
		off = line
		// Pull the section rule (and its spacer) above the cursor into view with
		// it: landing on a section's first binding with its heading scrolled off
		// loses the context that makes the row readable.
		for off > 0 && v.lines[off-1].row < 0 {
			off--
		}
	}
	if line >= off+visible {
		off = line - visible + 1
	}
	return clampHelpOffset(off, len(v.lines), visible)
}

// helpFooterHints are the footer hints shown while the panel is open. They are
// derived from the live bindings rather than written out: this footer is the
// only thing telling the user how to leave, and the sole mitigation for the
// panel deliberately not capturing quit, so a hardcoded "q" would lie under
// quit = ["Q"]. A literal "esc" is fine — that binding really is hardcoded.
func (m *Model) helpFooterHints() []footerHint {
	// Inside the filter prompt every printable key is text, so there is no quit
	// hint to give: the only ways out are the two listed here (and a
	// non-printable quit chord such as ctrl+c).
	if m.helpSearching {
		return []footerHint{{"⏎", "done"}, {"esc", "clear"}}
	}
	// "esc close" leads because the footer is truncated from the RIGHT
	// (padBetween), and on a narrow terminal mid-run the run-safety lead ahead
	// of these can push the tail off screen — the way out must be the hint that
	// survives.
	return []footerHint{
		{"esc", "close"},
		{formatKeys(firstKeys(m.keys.Up, m.keys.Down)), "select"},
		{"⏎", "run"},
		{"/", "search"},
		{m.keys.Quit.Help().Key, "quit"},
	}
}

// firstKeys takes each binding's primary key, so a hint reads "↑/↓" instead of
// every alias of both.
func firstKeys(bs ...key.Binding) []string {
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		if k := b.Keys(); len(k) > 0 {
			out = append(out, k[0])
		}
	}
	return out
}

// filterSections keeps the rows matching query — case-insensitive substring
// over the description, the displayed key text, AND the raw key names, so
// typing "enter" finds the rows the panel draws as "⏎". Sections left empty
// disappear. An empty query is the identity.
func filterSections(secs []helpSection, query string) []helpSection {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return secs
	}
	out := make([]helpSection, 0, len(secs))
	for _, s := range secs {
		rows := make([]helpRow, 0, len(s.rows))
		for _, r := range s.rows {
			if rowMatches(r, q) {
				rows = append(rows, r)
			}
		}
		if len(rows) > 0 {
			out = append(out, helpSection{title: s.title, rows: rows})
		}
	}
	return out
}

func rowMatches(r helpRow, lowerQuery string) bool {
	if strings.Contains(strings.ToLower(r.desc), lowerQuery) ||
		strings.Contains(strings.ToLower(r.keys), lowerQuery) ||
		strings.Contains(r.action, lowerQuery) {
		return true
	}
	for _, k := range r.rawKeys {
		if strings.Contains(strings.ToLower(k), lowerQuery) {
			return true
		}
	}
	return false
}

// helpCount is the bottom-border readout: which binding the cursor is on, out
// of how many are currently listed. Filtering changes the total, so it doubles
// as the match count.
func (m *Model) helpCount(v helpView) string {
	if v.shown == 0 {
		return "0 of 0"
	}
	return fmt.Sprintf("%d of %d", min(m.helpCursor, v.shown-1)+1, v.shown)
}

// helpRowAt returns the binding the cursor is on, if it is on one.
func (m *Model) helpRowAt(cursor int) (helpRow, bool) {
	i := 0
	for _, s := range filterSections(m.keys.helpSections(), m.helpQuery) {
		for _, r := range s.rows {
			if i == cursor {
				return r, true
			}
			i++
		}
	}
	return helpRow{}, false
}

// clampHelpOffset holds an offset inside the scrollable range.
func clampHelpOffset(off, total, visible int) int {
	return min(max(off, 0), max(0, total-visible))
}

// frameSize measures a rendered frame: its line count and its widest line.
func frameSize(frame string) (w, h int) {
	lines := strings.Split(frame, "\n")
	for _, l := range lines {
		w = max(w, ansi.StringWidth(l))
	}
	return w, len(lines)
}
