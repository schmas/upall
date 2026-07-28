package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	helpTitle = "Keybindings"
	helpMinW  = 30 // preferred floor, never applied past the frame's own width
	helpMaxW  = 72
	// helpQueryShown caps the query as drawn in the border, so it still fits
	// the count slot alongside the position indicator on a narrow terminal.
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
func (m *Model) helpLayout(frameW, frameH int) ([]string, rect) {
	secs := filterSections(m.keys.helpSections(), m.helpQuery)

	keyW, contentW := 0, 0
	noMatch := ""
	if len(secs) == 0 {
		noMatch = fmt.Sprintf("no match for %q", m.helpQuery)
		contentW = ansi.StringWidth(noMatch) + 1
	}
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

	// The cap always wins. clampInt is deliberately avoided here: it raises hi
	// to lo when the two invert, which on a 20-column terminal would hand back
	// a 30-wide panel.
	panelW := min(max(contentW+4, min(helpMinW, frameW)), min(frameW, helpMaxW))
	panelW = max(panelW, 2)

	var body []string
	if noMatch != "" {
		body = []string{" " + m.st.muted.Render(noMatch)}
	} else {
		body = helpBody(secs, keyW, panelW-2, m.st)
	}

	panelH := min(len(body)+2, max(3, frameH-2))
	panelH = min(panelH, max(2, frameH))

	return body, rect{
		x: max(0, (frameW-panelW)/2),
		y: max(0, (frameH-panelH)/2),
		w: panelW,
		h: panelH,
	}
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
	body, r := m.helpLayout(frameW, frameH)

	visible := max(0, r.h-2)
	off := clampHelpOffset(m.helpOffset, len(body), visible)
	// The min is required, not defensive: visible routinely exceeds the body
	// length (a one-line no-match body), and the panic would propagate out of
	// View and cancel a live run.
	end := min(off+visible, len(body))
	shown := body[off:end]

	box := titledBox(helpTitle, m.helpCount(off, end, len(body), visible),
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

// helpCount is the top-border indicator: the active filter and the visible
// slice of the body. The position is suppressed when everything fits, so a
// short list carries no noise.
func (m *Model) helpCount(off, end, total, visible int) string {
	pos := ""
	if visible > 0 && total > visible {
		pos = fmt.Sprintf("%d–%d of %d", off+1, end, total)
	}
	if !m.helpSearching && m.helpQuery == "" {
		return pos
	}
	// horizFill DROPS an over-long count instead of truncating it, so a long
	// query would vanish from the border while still filtering the list. Cap
	// what is DISPLAYED (the query itself is capped separately, at a length
	// that still exceeds a narrow border slot).
	q := "/" + ansi.Truncate(m.helpQuery, helpQueryShown, "…")
	if pos == "" {
		return q
	}
	return q + " · " + pos
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
