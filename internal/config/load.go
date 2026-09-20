package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DefaultSearchPaths are probed, in order, when no explicit path is given.
// The first file that exists wins; none existing is not an error, because
// running with no config file at all is the expected default.
var DefaultSearchPaths = []string{
	"pivot.yaml",
	"pivot.yml",
	filepath.Join("config", "pivot.yaml"),
	// Absolute and Unix-specific by nature; filepath.Join would only obscure it.
	"/etc/pivot/pivot.yaml",
}

// Options controls how [Load] assembles the configuration.
type Options struct {
	// Path is an explicit config file. Empty means search DefaultSearchPaths.
	// When set and missing, that is an error — the user asked for a specific
	// file and silently ignoring it would be worse than failing.
	Path string

	// Lookup resolves environment variables. Nil means [os.LookupEnv].
	// Injectable so tests never touch the real environment.
	Lookup func(string) (string, bool)

	// Override runs last and applies explicitly-set command-line flags. Only
	// flags the user actually typed should be applied here, so that an
	// untouched flag's default does not silently outrank an environment
	// variable or a config file.
	Override func(*Config)
}

// Result is the loaded configuration plus a record of where it came from,
// which is what `pivot config show` reports and what makes "why is this
// value what it is?" answerable.
type Result struct {
	Config *Config

	// SourceFile is the config file used, or "" if none was found.
	SourceFile string
}

// Load assembles configuration in precedence order, lowest first:
//
//	defaults  →  config file  →  environment  →  flags
//
// Each layer overwrites only the fields it specifies, so a config file setting
// the port does not reset the log level to its default.
func Load(opts Options) (*Result, error) {
	cfg := Default()
	res := &Result{Config: cfg}

	path, err := resolvePath(opts.Path)
	if err != nil {
		return nil, err
	}

	if path != "" {
		if err := applyFile(cfg, path); err != nil {
			return nil, err
		}
		res.SourceFile = path
	}

	lookup := opts.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}

	if err := applyEnv(cfg, lookup); err != nil {
		return nil, fmt.Errorf("environment: %w", err)
	}

	if opts.Override != nil {
		opts.Override(cfg)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return res, nil
}

// resolvePath returns the config file to read, or "" when there is none.
func resolvePath(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return "", fmt.Errorf("config file %q does not exist", explicit)
			}

			return "", fmt.Errorf("config file %q: %w", explicit, err)
		}

		return explicit, nil
	}

	for _, candidate := range DefaultSearchPaths {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", nil
}

// applyFile overlays a YAML file onto cfg.
func applyFile(cfg *Config, path string) error {
	// #nosec G304 -- the path is operator-supplied configuration, which is the
	// entire point of the flag.
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config file %q: %w", path, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	// Reject unknown keys: a typo like `prot: 9000` that silently does nothing
	// is a worse experience than a startup error naming the bad key.
	dec.KnownFields(true)

	if err := dec.Decode(cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return nil // empty file is fine
		}

		return fmt.Errorf("parse config file %q: %w", path, err)
	}

	return nil
}
