// Package sidecar carries the format-agnostic logic for YAML sidecar files: the
// <target>.clover.yaml (or .yml) that holds update rules for a target that
// cannot host an inline clover: comment, strict JSON being the canonical case.
// It owns the discovery rule (which names are sidecars and which target they
// govern), decoding a sidecar document into one directive per entry, and the
// find line-locator that resolves an entry to the single target line it
// rewrites. It depends only on directive and pattern, never on scan, so scan can
// import it without a cycle and remain the sole builder of scan.File.
package sidecar
