package tui

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// visualState walks s and reports the SGR parameter string and the OSC 8
// hyperlink target that are active at visible column col. It exists so the
// bleed tests can assert what a terminal would actually paint at a cell rather
// than asserting that a reset byte appears somewhere — the reset is
// unconditionally concatenated, so that check would pass even with the splice
// logic broken.
func visualState(s string, col int) (sgr, link string) {
	w := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if strings.HasPrefix(s[i:], "\x1b[") { // CSI … m
				j := i + 2
				for j < len(s) && (s[j] == ';' || s[j] == ':' || (s[j] >= '0' && s[j] <= '9')) {
					j++
				}
				if j < len(s) {
					if s[j] == 'm' {
						if p := s[i+2 : j]; p == "" || p == "0" {
							sgr = ""
						} else {
							sgr = p
						}
					}
					i = j + 1
					continue
				}
			}
			if strings.HasPrefix(s[i:], "\x1b]8;") { // OSC 8 ; params ; uri ST
				if end := strings.Index(s[i:], "\x1b\\"); end >= 0 {
					body := s[i+4 : i+end]
					link = ""
					if k := strings.Index(body, ";"); k >= 0 {
						link = body[k+1:]
					}
					i += end + 2
					continue
				}
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if w >= col {
			return sgr, link
		}
		w += ansi.StringWidth(string(r))
		i += size
	}
	return sgr, link
}

// assertSameShape is the compositor's whole contract: the frame it returns must
// be indistinguishable in shape from the one it was given.
func assertSameShape(t *testing.T, base, out, what string) {
	t.Helper()
	bl, ol := strings.Split(base, "\n"), strings.Split(out, "\n")
	if len(bl) != len(ol) {
		t.Fatalf("%s: line count %d, want %d", what, len(ol), len(bl))
	}
	for i := range bl {
		if got, want := ansi.StringWidth(ol[i]), ansi.StringWidth(bl[i]); got != want {
			t.Errorf("%s: line %d width %d, want %d (%q)", what, i, got, want, ol[i])
		}
	}
}

func TestOverlayPlain(t *testing.T) {
	base := "abcde\nfghij\nklmno"
	got := overlay(base, "XYZ\nUVW", 1, 1)
	want := "abcde\n" +
		"f" + spliceReset + "XYZ" + spliceReset + "j\n" +
		"k" + spliceReset + "UVW" + spliceReset + "o"
	if got != want {
		t.Errorf("overlay =\n%q\nwant\n%q", got, want)
	}
}

// TestOverlayWideGraphemeSweep is the regression test for the asymmetry between
// ansi.Truncate (drops a straddling cluster) and ansi.TruncateLeft (keeps it).
// A corpus of width-1 glyphs cannot fail this, which is exactly why the sweep
// uses CJK and emoji.
func TestOverlayWideGraphemeSweep(t *testing.T) {
	base := "a你b😀c\n你好世界😀\nplain ascii row"
	panel := "[##]\n[##]"
	maxW := 0
	for _, l := range strings.Split(base, "\n") {
		if w := ansi.StringWidth(l); w > maxW {
			maxW = w
		}
	}
	for x := -2; x <= maxW+2; x++ {
		for y := -1; y <= 2; y++ {
			assertSameShape(t, base, overlay(base, panel, x, y),
				"x="+strconv.Itoa(x)+" y="+strconv.Itoa(y))
		}
	}
}

func TestOverlayPreservesShape(t *testing.T) {
	panel := "PANEL\nPANEL"
	// Raw SGR rather than lipgloss: the test binary has no TTY, so lipgloss
	// degrades to the ASCII profile and would emit no escapes at all.
	styled := func(s string) string { return "\x1b[1;32m" + s + "\x1b[0m" }
	cases := []struct {
		name string
		base string
	}{
		{"plain", "0123456789\n0123456789\n0123456789\n0123456789"},
		{"styled", styled("0123456789") + "\n" + styled("abcdefghij") + "\n0123456789"},
		// The real frame is non-rectangular: renderFooterBar emits width-2 cells.
		{"ragged", "0123456789012\n0123456789012\n01234567890"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertSameShape(t, tc.base, overlay(tc.base, panel, 2, 1), tc.name)
		})
	}
}

func TestOverlayNoStyleBleed(t *testing.T) {
	const red = "\x1b[31m"
	base := red + "0123456789\x1b[0m\n" + red + "0123456789\x1b[0m"
	out := strings.Split(overlay(base, "AB\nCD", 3, 0), "\n")[0]

	if sgr, _ := visualState(out, 3); sgr != "" {
		t.Errorf("panel cell inherited SGR %q, want none", sgr)
	}
	if sgr, _ := visualState(out, 5); sgr == "" {
		t.Error("base segment right of the panel lost its color")
	}
	if sgr, _ := visualState(out, 0); sgr == "" {
		t.Error("base segment left of the panel lost its color")
	}
}

func TestOverlayNoHyperlinkBleed(t *testing.T) {
	const uri = "https://example.invalid/x"
	base := "\x1b]8;;" + uri + "\x1b\\0123456789\x1b]8;;\x1b\\"
	out := overlay(base, "AB", 3, 0)

	if _, link := visualState(out, 0); link != uri {
		t.Errorf("base link before the panel = %q, want %q", link, uri)
	}
	if _, link := visualState(out, 3); link != "" {
		t.Errorf("panel text is inside hyperlink %q", link)
	}
	if _, link := visualState(out, 6); link != uri {
		t.Errorf("base link right of the panel = %q, want it restored to %q", link, uri)
	}
}

func TestOverlayClipsOutOfRange(t *testing.T) {
	base := "abcde\nfghij"
	cases := []struct {
		name, panel string
		x, y        int
	}{
		{"panel wider and taller", "XXXXXXXXXX\nXXXXXXXXXX\nXXXXXXXXXX", 0, 0},
		{"negative origin", "XX\nXX", -5, -5},
		{"x past the base width", "XX", 99, 0},
		{"y past the last line", "XX", 0, 99},
		{"panel straddles the right edge", "XXXX", 3, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertSameShape(t, base, overlay(base, tc.panel, tc.x, tc.y), tc.name)
		})
	}
}

// A base line shorter than x must be left alone rather than padded out to the
// panel's column — growing it would break the frame's per-line width.
func TestOverlaySkipsShortBaseLine(t *testing.T) {
	base := "0123456789\nshort"
	out := strings.Split(overlay(base, "AB", 7, 0), "\n")
	if out[1] != "short" {
		t.Errorf("short line = %q, want it untouched", out[1])
	}
}
