// Package config loads and validates Clover's optional YAML configuration. Two
// layers share one shape and one embedded JSON schema: a user config under the
// XDG config dir, and a per-project .clover.yaml that overlays it. The schema is
// also published for editor tooling.
//
// Settings are grouped by the command they configure (run/lint/fmt), with a
// global block for cross-command defaults like output detail; per-command keys
// override the global one, and an explicit CLI flag overrides both.
package config

import (
	"bytes"
	"cmp"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/output"
	"github.com/gechr/clover/internal/version"
	"github.com/gechr/x/shell"
	xstrings "github.com/gechr/x/strings"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

//go:embed schema.json
var schemaJSON []byte

// projectFileNames are the per-project config file names tried, in order, when
// no explicit path is given.
var projectFileNames = []string{".clover.yaml", ".clover.yml"}

// userFileNames are the user-level config file names tried, in order, under the
// XDG config dir's clover subdirectory.
var userFileNames = []string{"config.yaml", "config.yml"}

// keyRequiredVersion is the config key name, used in error messages so they
// quote the key the user wrote rather than a paraphrase.
const keyRequiredVersion = "required-version"

// Config is a parsed .clover.yaml. Pointer fields are tri-state: nil means the
// key was absent (so a lower-precedence layer or built-in default applies),
// distinct from an explicit false/zero that overrides it.
type Config struct {
	RequiredVersion *string `yaml:"required-version"`
	Paths           Paths   `yaml:"paths"`
	Global          Global  `yaml:"global"`
	Run             Run     `yaml:"run"`
	Lint            Lint    `yaml:"lint"`
	Format          Format  `yaml:"fmt"`
}

// Paths controls which files clover scans.
type Paths struct {
	Exclude []string `yaml:"exclude"`
}

// Global holds cross-command defaults. A per-command block overrides these.
type Global struct {
	Output *output.Mode `yaml:"output"`
}

// Run holds defaults for `clover run`.
type Run struct {
	Verify     *bool        `yaml:"verify"`
	Prerelease *bool        `yaml:"prerelease"`
	Downgrade  *bool        `yaml:"downgrade"`
	Deep       *bool        `yaml:"deep"`
	Force      *bool        `yaml:"force"`
	Output     *output.Mode `yaml:"output"`
}

// Lint holds defaults for `clover lint`.
type Lint struct {
	Output *output.Mode `yaml:"output"`
}

// Format holds defaults for `clover fmt`.
type Format struct {
	Prune *bool `yaml:"prune"`
}

// Load reads the project config from path, or - when path is empty - the first
// of .clover.yaml/.clover.yml found in dir, validating it against the embedded
// schema. It returns nil with no error when no file is found and none was
// requested, so an absent config is simply no config.
func Load(dir, path string) (*Config, error) {
	return load(dir, path, projectFileNames)
}

// LoadUser reads the user config from <config-dir>/clover/config.yaml (or
// .yml), where <config-dir> is $XDG_CONFIG_HOME or its per-OS default (e.g.
// ~/.config). Like Load it returns nil with no error when no file is present.
// Its settings form the base that a project config overlays via Merge.
func LoadUser() (*Config, error) {
	dir, err := userDir()
	if err != nil {
		return nil, err
	}
	return load(dir, "", userFileNames)
}

// Merge returns the effective config: the user config overlaid by the project
// one, field by field. The project layer always takes precedence - any field it
// sets wins, and only the fields it leaves unset fall back to user. Either
// argument may be nil. Configs are treated as immutable after load: the result
// is a fresh top-level value, but its pointer and slice leaves may alias an
// input's, so callers must not mutate them in place.
func Merge(user, project *Config) *Config {
	switch {
	case user == nil:
		return project
	case project == nil:
		return user
	}
	merged := *user
	merged.RequiredVersion = cmp.Or(project.RequiredVersion, merged.RequiredVersion)
	if project.Paths.Exclude != nil {
		merged.Paths.Exclude = project.Paths.Exclude
	}
	merged.Global.Output = cmp.Or(project.Global.Output, merged.Global.Output)
	merged.Run.Verify = cmp.Or(project.Run.Verify, merged.Run.Verify)
	merged.Run.Prerelease = cmp.Or(project.Run.Prerelease, merged.Run.Prerelease)
	merged.Run.Downgrade = cmp.Or(project.Run.Downgrade, merged.Run.Downgrade)
	merged.Run.Deep = cmp.Or(project.Run.Deep, merged.Run.Deep)
	merged.Run.Force = cmp.Or(project.Run.Force, merged.Run.Force)
	merged.Run.Output = cmp.Or(project.Run.Output, merged.Run.Output)
	merged.Lint.Output = cmp.Or(project.Lint.Output, merged.Lint.Output)
	merged.Format.Prune = cmp.Or(project.Format.Prune, merged.Format.Prune)
	return &merged
}

// load reads, schema-validates, and unmarshals the first of names found in dir
// (or path when given), shared by the user and project layers.
func load(dir, path string, names []string) (*Config, error) {
	data, source, err := read(dir, path, names)
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
	// The schema validates the types and enums of known keys (a hard error);
	// unknown keys are tolerated here and warned about below, so a config written
	// for a newer clover still loads on an older one.
	if err := schema.Validate(raw); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}

	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		var typeErr *yaml.TypeError
		switch {
		case errors.As(err, &typeErr):
			// KnownFields turns every unrecognized key into a "not found" entry;
			// the known keys still decode, so we warn and carry on.
			for _, msg := range typeErr.Errors {
				warnUnknownKey(source, unknownField(msg))
			}
		case errors.Is(err, io.EOF):
			// A comment-only document decodes to nothing; the zero Config is valid.
		default:
			return nil, fmt.Errorf("parse %s: %w", source, err)
		}
	}
	if err := validateValues(&cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	return &cfg, nil
}

// warnUnknownKey logs an ignored config key, offering the nearest known key as a
// "did you mean?" hint when the unknown one is a plausible typo.
func warnUnknownKey(source, key string) {
	event := clog.Warn().Path(field.Path, source).Str(field.Key, key)
	if near := xstrings.Closest(key, knownKeys); near != "" {
		event = event.Str(field.Hint, fmt.Sprintf("did you mean %q?", near))
	}
	event.Msg("Ignoring unknown config key")
}

// validateValues checks the values the schema cannot express: that a present
// required-version is a parseable constraint, caught at load rather than left
// for the version gate (which a dev build skips).
func validateValues(cfg *Config) error {
	if rv := cfg.requiredVersion(); rv != "" {
		if _, err := version.NewConstraint(rv, sentinelVersion); err != nil {
			return fmt.Errorf("invalid %q constraint %q: %w", keyRequiredVersion, rv, err)
		}
	}
	return nil
}

// sentinelVersion is an arbitrary parseable version, supplied to NewConstraint
// when validating constraint syntax at load - a range ignores it, so only the
// expression's well-formedness is checked.
var sentinelVersion = func() *version.Version {
	v, err := version.Parse("0.0.0")
	if err != nil {
		panic(fmt.Sprintf("config: sentinel version does not parse: %v", err))
	}
	return v
}()

// knownKeys is the flattened set of recognized config keys, derived once from
// the Config struct's yaml tags so a new field needs no parallel list to feed
// typo suggestions.
var knownKeys = configKeys(reflect.TypeFor[Config]())

// configKeys walks t's yaml-tagged fields recursively, collecting every key
// name. Duplicate leaf names (e.g. output under several blocks) are harmless for
// suggestion matching, so they are not deduplicated.
func configKeys(t reflect.Type) []string {
	var keys []string
	for f := range t.Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue
		}
		keys = append(keys, name)
		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			keys = append(keys, configKeys(ft)...)
		}
	}
	return keys
}

// unknownField extracts the key name from a yaml KnownFields error such as
// "line 3: field verfy not found in type config.Run", falling back to the raw
// message when the shape is unexpected.
func unknownField(msg string) string {
	const prefix, suffix = "field ", " not found"
	i := strings.Index(msg, prefix)
	j := strings.Index(msg, suffix)
	if i < 0 || j < 0 || j < i {
		return msg
	}
	return msg[i+len(prefix) : j]
}

// userDir returns the clover subdirectory of the user's XDG config directory.
func userDir() (string, error) {
	base, err := shell.ConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate config dir: %w", err)
	}
	return filepath.Join(base, "clover"), nil
}

// ExcludeGlobs returns the configured exclude globs, nil-safe so callers need
// not branch on a missing config.
func (c *Config) ExcludeGlobs() []string {
	if c == nil {
		return nil
	}
	return c.Paths.Exclude
}

// Verify, Prerelease, Downgrade, and Deep return the configured run defaults as
// tri-state pointers (nil = unset), nil-safe on the config. A caller resolves
// the effective value as cmp.Or(cliFlag, cfg.Verify()): the CLI flag wins, then
// the config, then the per-marker directive that a nil leaves untouched.
func (c *Config) Verify() *bool { return c.run().Verify }

// Prerelease returns the configured run.prerelease default (nil = unset).
func (c *Config) Prerelease() *bool { return c.run().Prerelease }

// Downgrade returns the configured run.downgrade default (nil = unset).
func (c *Config) Downgrade() *bool { return c.run().Downgrade }

// Deep returns the configured run.deep default (nil = unset).
func (c *Config) Deep() *bool { return c.run().Deep }

// Force returns the configured run.force default (nil = unset). When true, a
// followed digest is re-pinned even if the version it follows is unchanged.
func (c *Config) Force() *bool { return c.run().Force }

// Prune returns the configured fmt.prune default (nil = unset).
func (c *Config) Prune() *bool {
	if c == nil {
		return nil
	}
	return c.Format.Prune
}

// RunOutput resolves the output detail for `clover run`: the CLI value when set,
// then run.output, then global.output, then the built-in text default.
func (c *Config) RunOutput(cli *output.Mode) output.Mode {
	return c.output(cli, c.run().Output)
}

// LintOutput resolves the output detail for `clover lint`, with the same
// precedence as [Config.RunOutput] but keyed on lint.output.
func (c *Config) LintOutput(cli *output.Mode) output.Mode {
	if c == nil {
		return derefOutput(cli)
	}
	return c.output(cli, c.Lint.Output)
}

// output applies the CLI > command > global > text precedence, given the
// already-selected command-level value.
func (c *Config) output(cli, command *output.Mode) output.Mode {
	var global *output.Mode
	if c != nil {
		global = c.Global.Output
	}
	return derefOutput(cmp.Or(cli, command, global))
}

// derefOutput dereferences a resolved output pointer, defaulting to text.
func derefOutput(o *output.Mode) output.Mode {
	if o == nil {
		return output.Text
	}
	return *o
}

// run returns the run block, nil-safe so the accessors need not branch.
func (c *Config) run() Run {
	if c == nil {
		return Run{}
	}
	return c.Run
}

// CheckVersion verifies the running clover version satisfies required-version.
// An empty constraint, or a current version that does not parse (a dev build),
// passes - the gate never blocks an unversioned binary.
func (c *Config) CheckVersion(current string) error {
	rv := c.requiredVersion()
	if rv == "" {
		return nil
	}
	parsed, err := version.Parse(current)
	if err != nil {
		return nil //nolint:nilerr // an unparseable (dev) version skips the gate
	}
	constraint, err := version.NewConstraint(rv, parsed)
	if err != nil {
		return fmt.Errorf("invalid %q constraint %q: %w", "required-version", rv, err)
	}
	if !constraint.Allowed(parsed) {
		return fmt.Errorf(
			"clover %s does not satisfy the %q constraint %q",
			current,
			keyRequiredVersion,
			rv,
		)
	}
	return nil
}

// requiredVersion returns the configured constraint, "" when unset or on a nil
// config, collapsing the tri-state pointer to the value the gate works with.
func (c *Config) requiredVersion() string {
	if c == nil || c.RequiredVersion == nil {
		return ""
	}
	return *c.RequiredVersion
}

// read returns the config bytes and its full path, used both as a label in
// messages and as a linkable path in warnings. With an explicit path a missing
// file is an error; otherwise names are tried in dir and a miss returns nil
// bytes.
func read(dir, path string, names []string) ([]byte, string, error) {
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, path, fmt.Errorf("read config: %w", err)
		}
		return data, path, nil
	}

	for _, name := range names {
		full := filepath.Join(dir, name)
		data, err := os.ReadFile(full)
		switch {
		case err == nil:
			return data, full, nil
		case errors.Is(err, os.ErrNotExist):
			continue
		default:
			return nil, full, fmt.Errorf("read config: %w", err)
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
