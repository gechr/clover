package provider

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/x/set"
	xstrings "github.com/gechr/x/strings"
)

// Filter selects which providers a run resolves, matched against the concrete
// provider each marker resolves to (a provider=auto marker after inference, a
// follower after the producer it follows). An enable set is authoritative -
// only markers whose provider is named run - while a disable set is subtractive
// - every provider except those named. The two are mutually exclusive, and the
// zero Filter matches everything. The manual provider is never gated: it
// publishes the value already on its line and updates nothing, so it always runs.
type Filter struct {
	enable  set.Set[string]
	disable set.Set[string]
}

// NewFilter builds a provider Filter from the --enable and --disable names. Each
// value may be repeated or comma-separated. Unknown providers are rejected, as
// is the manual provider (which always runs) and combining both lists, since one
// selects and the other deselects.
func NewFilter(enable, disable []string) (Filter, error) {
	on, err := selection("enable", enable)
	if err != nil {
		return Filter{}, err
	}
	off, err := selection("disable", disable)
	if err != nil {
		return Filter{}, err
	}
	if len(on) > 0 && len(off) > 0 {
		return Filter{}, fmt.Errorf("--enable and --disable cannot be combined")
	}
	return Filter{enable: on, disable: off}, nil
}

// Empty reports whether the filter selects nothing, in which case every marker
// runs.
func (f Filter) Empty() bool { return len(f.enable) == 0 && len(f.disable) == 0 }

// Match reports whether a marker resolving to provider runs under the filter.
// The manual provider always runs; an enable set admits only its members, a
// disable set admits everything but its members, and the zero filter admits all.
func (f Filter) Match(provider string) bool {
	if provider == constant.ProviderManual {
		return true
	}
	if len(f.enable) > 0 {
		return f.enable.Contains(provider)
	}
	return !f.disable.Contains(provider)
}

// String renders the active selection for logging, e.g. "only github, docker" or
// "all except node". The zero filter renders empty.
func (f Filter) String() string {
	switch {
	case len(f.enable) > 0:
		return "only " + strings.Join(set.SortedNatural(f.enable), ", ")
	case len(f.disable) > 0:
		return "all except " + strings.Join(set.SortedNatural(f.disable), ", ")
	default:
		return ""
	}
}

// selection parses one flag's provider names into a set, validating each against
// [Selectable].
func selection(flag string, values []string) (set.Set[string], error) {
	valid := Selectable()
	names := set.New[string]()
	for _, value := range values {
		for _, name := range xstrings.SplitBy(value, ",") {
			if !slices.Contains(valid, name) {
				return nil, fmt.Errorf(
					"unknown provider %q for --%s, valid providers: %s",
					name, flag, strings.Join(valid, ", "),
				)
			}
			names.Add(name)
		}
	}
	return names, nil
}

// Selectable is the sorted set of provider names --enable and --disable accept:
// every registered provider except manual, which always runs and so cannot be
// selected or deselected.
func Selectable() []string {
	return slices.DeleteFunc(Names(), func(name string) bool {
		return name == constant.ProviderManual
	})
}
