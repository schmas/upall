package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
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
	// helpQueryShown caps the query as drawn in the border, so it still fits the
	// count slot alongside the match count on a narrow terminal.
	helpQueryShown = 20
)

// helpRow is one listed binding. keys is what the panel prints (glyphs), while
// rawKeys keeps the tea key names behind it: the displayed "⏎" is unsearchable,
// so the filter (phase 4) matches against both.
type helpRow struct {
	keys    string
	rawKeys []string
	desc    string
}

// helpSection is a labeled group of rows, drawn under a centered rule.
type helpSection struct {
	title string
	rows  []helpRow
}

// helpView is one laid-out panel: the body to draw, the box to draw it in, and
// the row counts behind the border's indicator. shown and total are counted in
// BINDINGS rather than body lines — "12 of 46" answers what the filter did,
// which a line count (section rules and spacers included) cannot.
type helpView struct {
	body         []string
	rect         rect
	shown, total int
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
	row := func(b key.Binding) helpRow {
		return helpRow{keys: b.Help().Key, rawKeys: b.Keys(), desc: b.Help().Desc}
	}
	// Literal rows for keys that are genuinely hardcoded (not rebindable
	// actions) — the one sanctioned exception to deriving from the keymap.
	lit := func(keys, desc string) helpRow {
		return helpRow{keys: keys, rawKeys: []string{keys}, desc: desc}
	}
	return []helpSection{
		{"Navigation", []helpRow{row(k.Up), row(k.Down), row(k.Top), row(k.Bottom)}},
		{"Panes & Focus", []helpRow{row(k.FocusNext), row(k.FocusPrev)}},
		{"Steps", []helpRow{row(k.Start), row(k.Follow), row(k.Toggle), row(k.FilterPrev), row(k.FilterNext)}},
		{"Run Control", []helpRow{row(k.Retry), row(k.Continue), row(k.Restart), row(k.Stop), row(k.TypeMode)}},
		{"Output & Pager", []helpRow{row(k.All), row(k.Wrap), row(k.Pager)}},
		{"History", []helpRow{row(k.Expand), row(k.Collapse)}},
		{"Config & Tools", []helpRow{row(k.OpenConfig), row(k.OpenConfigDir), row(k.SelfUpdate)}},
		{"General", []helpRow{row(k.Help), row(k.Quit)}},
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
	var body []string
	if len(secs) == 0 {
		body = []string{" " + m.st.muted.Render(fmt.Sprintf("no match for %q", m.helpQuery))}
	} else {
		body = helpBody(secs, keyW, panelW-2, m.st)
	}

	return helpView{
		body:  body,
		shown: countRows(secs),
		total: countRows(all),
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

// helpBody lays the sections out as inner box lines: a centered section rule,
// then one right-aligned key column plus description per row, with a blank
// spacer between sections.
func helpBody(secs []helpSection, keyW, innerW int, st styles) []string {
	var body []string
	for i, s := range secs {
		if i > 0 {
			body = append(body, "")
		}
		rule := st.sep.Render("───") + " " + st.selected.Render(s.title) + " " + st.sep.Render("───")
		body = append(body, lipgloss.PlaceHorizontal(innerW, lipgloss.Center, rule))
		for _, r := range s.rows {
			pad := max(0, keyW-ansi.StringWidth(r.keys))
			body = append(body, " "+strings.Repeat(" ", pad)+
				st.keycap.Render(r.keys)+"  "+st.muted.Render(r.desc))
		}
	}
	return body
}

// helpOverlay renders the panel over frame and returns the composited frame,
// unchanged in line count and per-line width (see overlay).
func (m *Model) helpOverlay(frame string) string {
	frameW, frameH := frameSize(frame)
	v := m.helpLayout(frameW, frameH)
	body, r := v.body, v.rect

	visible := max(0, r.h-2)
	off := clampHelpOffset(m.helpOffset, len(body), visible)
	// The min is required, not defensive: visible routinely exceeds the body
	// length (a one-line no-match body), and the panic would propagate out of
	// View and cancel a live run.
	end := min(off+visible, len(body))
	shown := body[off:end]

	box := titledBox(helpTitle, m.helpCount(v),
		strings.Join(shown, "\n"), true, r.w, r.h, m.st,
		scrollbarThumb(visible, len(body), visible, off))
	return overlay(frame, box, r.x, r.y)
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
		{formatKeys(firstKeys(m.keys.Up, m.keys.Down)), "scroll"},
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
		strings.Contains(strings.ToLower(r.keys), lowerQuery) {
		return true
	}
	for _, k := range r.rawKeys {
		if strings.Contains(strings.ToLower(k), lowerQuery) {
			return true
		}
	}
	return false
}

// helpCount is the top-border indicator. It is always present — how many
// bindings the panel is listing is worth knowing before you start filtering,
// and it is the readout that answers what a filter just did. Scroll position
// is the scrollbar thumb's job, not this slot's.
func (m *Model) helpCount(v helpView) string {
	count := strconv.Itoa(v.total)
	if v.shown != v.total {
		count = fmt.Sprintf("%d of %d", v.shown, v.total)
	}
	if !m.helpSearching && m.helpQuery == "" {
		return count
	}
	// horizFill DROPS an over-long count instead of truncating it, so a long
	// query would vanish from the border while still filtering the list. Cap
	// what is DISPLAYED (the query itself is capped separately, at a length
	// that still exceeds a narrow border slot).
	return "/" + ansi.Truncate(m.helpQuery, helpQueryShown, "…") + " · " + count
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
