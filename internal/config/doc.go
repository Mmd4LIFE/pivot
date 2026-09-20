// Package config loads and validates Pivot's configuration.
//
// Precedence, highest first: command-line flags, PIVOT_* environment
// variables, a config file, then built-in defaults. Defaults must be good
// enough that `./pivot` works with no configuration at all — that is the
// 30-second-install commitment from docs/vision.md.
//
// Validation happens once, at startup, with errors that name the offending
// key and say what a valid value looks like. Nothing downstream should have
// to defend against a malformed config, and every problem is reported in one
// pass rather than one per restart.
//
// Adding a field means four things: a struct tag, a default in Default(), a
// binding in env.go, and a rule in Validate. Skipping any of them leaves a
// field that cannot be set, cannot be discovered, or cannot be trusted.
package config
