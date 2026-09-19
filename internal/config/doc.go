// Package config loads and validates Pivot's configuration.
//
// Precedence, highest first: command-line flags, PIVOT_* environment
// variables, a config file, then built-in defaults. Defaults must be good
// enough that `./pivot` works with no configuration at all — that is the
// 30-second-install commitment from docs/vision.md.
//
// Validation happens once, at startup, with errors that name the offending
// key and say what a valid value looks like. Nothing downstream should have
// to defend against a malformed config.
//
// Built in Part 2.
package config
