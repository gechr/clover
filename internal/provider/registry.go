package provider

import "slices"

// registry is the compiled-in set of providers. It is populated from provider
// packages' init functions, so importing a provider package registers it - the
// one sanctioned mutable global, written only during init.
var registry = map[string]Provider{}

// Register adds p under its name. Providers call this from init(); a duplicate
// name overwrites, which only happens if two providers claim the same name.
func Register(p Provider) {
	registry[p.Name()] = p
}

// Get returns the registered provider with the given name.
func Get(name string) (Provider, bool) {
	p, ok := registry[name]
	return p, ok
}

// Names returns the registered provider names, sorted for stable output.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
