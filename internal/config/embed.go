package config

import "embed"

// defaultsFS holds the embedded default plugins. Their run order is set by each
// step's explicit `order`, never by the alphabetical //go:embed glob order.
//
//go:embed defaults/*.toml
var defaultsFS embed.FS
