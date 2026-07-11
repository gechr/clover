package provider

import xmaps "github.com/gechr/x/maps"

// registry is the compiled-in set of providers. It is populated explicitly at
// startup via [RegisterAll] from the composition root, not by import side
// effects - the one sanctioned mutable global, written once before any lookup.
var registry = map[string]Provider{}

// RegisterAll adds each provider under its name. The composition root calls it
// once at startup with the built-in providers, so registration is explicit
// rather than hidden in package init functions.
func RegisterAll(providers ...Provider) {
	for _, p := range providers {
		Register(p)
	}
}

// Register adds p under its name. A duplicate name overwrites, which only
// happens if two providers claim the same name.
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
	return xmaps.KeysNatural(registry)
}
