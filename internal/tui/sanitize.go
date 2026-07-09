package tui

import (
	"bytes"
	"regexp"
)

// stripRE matches the escape sequences that must NOT reach the alt screen: CSI
// sequences other than SGR color (cursor moves, line erases like ESC[K, column
// jumps like ESC[G — what brew/mise emit to redraw progress) and OSC sequences
// (window-title sets). SGR (final byte 'm') is kept so colors survive.
var stripRE = regexp.MustCompile(
	"\x1b\\[[0-9;?]*[ -/]*[@-ln-~]" + // CSI, any final byte except 'm' (SGR)
		"|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)", // OSC … BEL or ST
)

// sanitize makes one captured output line safe to render inside the log pane.
// Progress redraws (carriage returns, cursor/erase escapes) would otherwise move
// the physical cursor and paint outside the viewport — over the master pane on
// the left, or past the right edge. It collapses \r redraws to the final on-screen
// segment, strips the non-color escapes, then drops any remaining C0 control byte
// (except tab) and any bare ESC — while preserving the SGR color sequences that
// survived stripRE. The raw stream still reaches the on-disk logfile untouched via
// the runner's tee, so nothing is lost.
func sanitize(line []byte) []byte {
	// Collapse a carriage-return progress redraw: keep only what would remain on
	// screen after the last \r, ignoring a trailing \r from a bare CRLF.
	line = bytes.TrimRight(line, "\r")
	if i := bytes.LastIndexByte(line, '\r'); i >= 0 {
		line = line[i+1:]
	}
	line = stripRE.ReplaceAll(line, nil) // always returns a freshly-owned slice
	out := line[:0]                      // filter in place: out index never passes i
	for i := 0; i < len(line); i++ {
		b := line[i]
		if b == 0x1b {
			// Keep ESC only as an SGR introducer (ESC[…m survived stripRE); drop a
			// bare/stray ESC so it cannot swallow the following byte on the terminal.
			if i+1 < len(line) && line[i+1] == '[' {
				out = append(out, b)
			}
			continue
		}
		if b == '\t' || b >= 0x20 {
			out = append(out, b)
		}
	}
	return out
}
