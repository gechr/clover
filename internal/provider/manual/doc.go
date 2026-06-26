// Package manual implements a human-owned root provider. Unlike the upstream
// providers, it resolves nothing over the network: its value is whatever the
// target line already carries, published under the marker's id so followers and
// side values track it coherently. clover never rewrites a manual line - a
// person owns the value - so the provider exists to anchor a dependency graph
// at a value no registry knows about.
package manual
