// Command upall updates every installed toolchain (Homebrew, mise, rust, ...)
// through a config-driven step engine, rendered as a Bubble Tea TUI.
//
// This is a Phase 1 placeholder; the real CLI is wired in Phase 2.
package main

import "fmt"

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	fmt.Printf("upall %s (engine scaffold)\n", version)
}
