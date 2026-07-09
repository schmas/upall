package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSanitizeCollapsesCarriageReturns(t *testing.T) {
	// Homebrew cask progress redraws the same line with \r; we want the final state.
	in := []byte("Downloading 16MB\rDownloading 160MB\rDownloaded 333MB")
	if got := string(sanitize(in)); got != "Downloaded 333MB" {
		t.Fatalf("sanitize = %q, want final segment", got)
	}
}

func TestSanitizeTrailingCR(t *testing.T) {
	if got := string(sanitize([]byte("hello\r"))); got != "hello" {
		t.Fatalf("sanitize = %q, want hello", got)
	}
}

func TestSanitizeKeepsColorStripsCursor(t *testing.T) {
	// SGR (color) survives; a line-erase / cursor move is removed.
	in := []byte("\x1b[33mWARN\x1b[0m keep\x1b[2K\x1b[1G")
	got := string(sanitize(in))
	want := "\x1b[33mWARN\x1b[0m keep"
	if got != want {
		t.Fatalf("sanitize = %q, want %q", got, want)
	}
}

func TestSanitizeThenWrapConstrainsWidth(t *testing.T) {
	// A long mise WARN line must wrap to the pane width once sanitized — no line
	// may exceed the limit (this is the right-edge overflow the box showed).
	in := []byte("\x1b[33mmise WARN\x1b[0m  newer cargo:cargo-update release 21.0.1 (released 2026-07-09) ignored by minimum_release_age (24h); latest eligible release is 20.0.3")
	const w = 40
	wrapped := ansi.Hardwrap(string(sanitize(in)), w, true)
	for _, ln := range strings.Split(wrapped, "\n") {
		if pw := ansi.StringWidth(ln); pw > w {
			t.Fatalf("wrapped line width %d exceeds %d: %q", pw, w, ln)
		}
	}
}
