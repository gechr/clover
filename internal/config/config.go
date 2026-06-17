// Package config loads and validates the optional .clover.yaml project file: a
// required-version gate and the paths clover scans. The file is validated
// against an embedded JSON schema, which is also published for editor tooling.
package config

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gechr/clover/internal/version"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

//go:embed schema.json
var schemaJSON []byte

// fileNames are the config file names tried, in order, when no explicit path is
// given.
var fileNames = []string{".clover.yaml", ".clover.yml"}

// Config is a parsed .clover.yaml.
type Config struct {
	RequiredVersion string `yaml:"required-version"`
	Paths           Paths  `yaml:"paths"`
}

// Paths controls which files clover scans.
type Paths struct {
	Exclude []string `yaml:"exclude"`
}

// Load reads the config from path, or - when path is empty - the first of
// .clover.yaml/.clover.yml found in dir, validating it against the embedded
// schema. It returns nil with no error when no file is found and none was
// requested, so an absent config is simply no config.
func Load(dir, path string) (*Config, error) {
	data, source, err := read(dir, path)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil //nolint:nilnil // absent config is not an error
	}

	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", source, err)
	}
	if err := schema.Validate(raw); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", source, err)
	}
	return &cfg, nil
}

// ExcludeGlobs returns the configured exclude globs, nil-safe so callers need
// not branch on a missing config.
func (c *Config) ExcludeGlobs() []string {
	if c == nil {
		return nil
	}
	return c.Paths.Exclude
}

// CheckVersion verifies the running clover version satisfies required-version.
// An empty constraint, or a current version that does not parse (a dev build),
// passes - the gate never blocks an unversioned binary.
func (c *Config) CheckVersion(current string) error {
	if c == nil || c.RequiredVersion == "" {
		return nil
	}
	parsed, err := version.Parse(current)
	if err != nil {
		return nil //nolint:nilerr // an unparseable (dev) version skips the gate
	}
	constraint, err := version.NewConstraint(c.RequiredVersion, parsed)
	if err != nil {
		return fmt.Errorf("invalid required-version %q: %w", c.RequiredVersion, err)
	}
	if !constraint.Allowed(parsed) {
		return fmt.Errorf(
			"clover %s does not satisfy required-version %q",
			current,
			c.RequiredVersion,
		)
	}
	return nil
}

// read returns the config bytes and a label for messages. With an explicit path
// a missing file is an error; otherwise the file names are tried in dir and a
// miss returns nil bytes.
func read(dir, path string) ([]byte, string, error) {
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, path, fmt.Errorf("read config: %w", err)
		}
		return data, path, nil
	}

	for _, name := range fileNames {
		full := filepath.Join(dir, name)
		data, err := os.ReadFile(full)
		switch {
		case err == nil:
			return data, name, nil
		case errors.Is(err, os.ErrNotExist):
			continue
		default:
			return nil, name, fmt.Errorf("read config: %w", err)
		}
	}
	return nil, "", nil
}

// schema is the compiled config schema, built once from the embedded document.
var schema = compileSchema()

func compileSchema() *jsonschema.Schema {
	var doc any
	if err := json.Unmarshal(schemaJSON, &doc); err != nil {
		panic(fmt.Sprintf("config: embedded schema is not valid JSON: %v", err))
	}
	compiler := jsonschema.NewCompiler()
	const id = "clover.schema.json"
	if err := compiler.AddResource(id, doc); err != nil {
		panic(fmt.Sprintf("config: cannot add schema resource: %v", err))
	}
	compiled, err := compiler.Compile(id)
	if err != nil {
		panic(fmt.Sprintf("config: cannot compile schema: %v", err))
	}
	return compiled
}
