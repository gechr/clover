package config

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/gechr/x/human"
)

// Marker describes one marker for scoped-rule matching: the file path relative
// to its repository root in slash form, the resolved provider name, and the
// directive's tags.
type Marker struct {
	Path     string
	Provider string
	Tags     []string
}

// Rule scopes run defaults to the markers its selectors match. At least one
// selector (paths, providers, tags) must be set. A setting left unset falls
// through to the next matching rule and then to the run block's own default,
// so orthogonal rules compose instead of shadowing each other.
type Rule struct {
	Paths     []string `yaml:"paths"`
	Providers []string `yaml:"providers"`
	Tags      []string `yaml:"tags"`

	Cooldown   *string `yaml:"cooldown"`
	Deep       *bool   `yaml:"deep"`
	Downgrade  *bool   `yaml:"downgrade"`
	Force      *bool   `yaml:"force"`
	Prerelease *bool   `yaml:"prerelease"`
	Verify     *bool   `yaml:"verify"`
}

// matches reports whether every set selector accepts m: some paths glob matches
// m.Path, providers contains m.Provider, and every rule tag is among m.Tags. An
// absent selector accepts everything. Matching is case-insensitive for names
// and tags, mirroring the --tag filter.
func (r Rule) matches(m Marker) bool {
	if len(r.Paths) > 0 && !slices.ContainsFunc(r.Paths, func(glob string) bool {
		return doublestar.ValidatePattern(glob) && doublestar.MatchUnvalidated(glob, m.Path)
	}) {
		return false
	}
	if len(r.Providers) > 0 && !slices.ContainsFunc(r.Providers, func(name string) bool {
		return strings.EqualFold(name, m.Provider)
	}) {
		return false
	}
	for _, want := range r.Tags {
		if !slices.ContainsFunc(m.Tags, func(tag string) bool {
			return strings.EqualFold(tag, want)
		}) {
			return false
		}
	}
	return true
}

// validate checks the shape the schema cannot express: a rule with no selector
// would silently apply everywhere, one with no settings is inert, and globs and
// cooldowns must parse. i names the rule in the error.
func (r Rule) validate(i int) error {
	if len(r.Paths) == 0 && len(r.Providers) == 0 && len(r.Tags) == 0 {
		return fmt.Errorf(
			"%q needs at least one of %q, %q, or %q",
			ruleKey(i, ""), "paths", "providers", "tags",
		)
	}
	if r.Verify == nil && r.Prerelease == nil && r.Downgrade == nil &&
		r.Deep == nil && r.Force == nil && r.Cooldown == nil {
		return fmt.Errorf("%q sets no defaults", ruleKey(i, ""))
	}
	for _, glob := range r.Paths {
		if !doublestar.ValidatePattern(glob) {
			return fmt.Errorf("invalid %q glob %q", ruleKey(i, "paths"), glob)
		}
	}
	if r.Cooldown != nil {
		if _, err := human.ParseDuration(*r.Cooldown); err != nil {
			return fmt.Errorf(
				"%q must be a duration like 2w3d, got %q",
				ruleKey(i, "cooldown"), *r.Cooldown,
			)
		}
	}
	return nil
}

// ruleKey renders the config key naming rule i (optionally one of its fields)
// in error messages, so they quote the key the user wrote.
func ruleKey(i int, field string) string {
	key := fmt.Sprintf("run.rules[%d]", i)
	if field != "" {
		key += "." + field
	}
	return key
}

// VerifyFor resolves the run.verify default for marker m: the first matching
// rule that sets verify wins, else the run block's own value. Like every
// scoped accessor it is nil-safe on the config.
func (c *Config) VerifyFor(m Marker) *bool {
	return c.scopedBool(m, func(r Rule) *bool { return r.Verify }, c.run().Verify)
}

// PrereleaseFor resolves the run.prerelease default for marker m.
func (c *Config) PrereleaseFor(m Marker) *bool {
	return c.scopedBool(m, func(r Rule) *bool { return r.Prerelease }, c.run().Prerelease)
}

// DowngradeFor resolves the run.downgrade default for marker m.
func (c *Config) DowngradeFor(m Marker) *bool {
	return c.scopedBool(m, func(r Rule) *bool { return r.Downgrade }, c.run().Downgrade)
}

// DeepFor resolves the run.deep default for marker m.
func (c *Config) DeepFor(m Marker) *bool {
	return c.scopedBool(m, func(r Rule) *bool { return r.Deep }, c.run().Deep)
}

// ForceFor resolves the run.force default for marker m.
func (c *Config) ForceFor(m Marker) *bool {
	return c.scopedBool(m, func(r Rule) *bool { return r.Force }, c.run().Force)
}

// CooldownFor resolves the run.cooldown default for marker m: the first
// matching rule that sets cooldown wins, else run.cooldown, zero when neither
// applies. Like [Config.Cooldown] it only fills in for directives carrying no
// cooldown of their own. Values are validated at load, so parsing cannot fail
// here.
func (c *Config) CooldownFor(m Marker) time.Duration {
	for _, r := range c.run().Rules {
		if r.Cooldown != nil && r.matches(m) {
			d, _ := human.ParseDuration(*r.Cooldown)
			return d
		}
	}
	return c.Cooldown()
}

// scopedBool returns the setting selected by field from the first rule that
// both matches m and sets it, else the run block's global value.
func (c *Config) scopedBool(m Marker, field func(Rule) *bool, global *bool) *bool {
	for _, r := range c.run().Rules {
		if v := field(r); v != nil && r.matches(m) {
			return v
		}
	}
	return global
}
