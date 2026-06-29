// Package jq compiles jq programs, so the gojq parse-then-compile pair lives in
// one place for every caller that queries JSON - the http provider's extractor
// and the sidecar locator alike. It deliberately adds no error context: the
// caller knows whether the program came from an http jq= or a sidecar locator
// and wraps the error accordingly.
package jq

import "github.com/itchyny/gojq"

// Compile parses and compiles a jq program, returning the gojq error unwrapped
// so the caller can frame it in its own terms.
func Compile(expr string) (*gojq.Code, error) {
	query, err := gojq.Parse(expr)
	if err != nil {
		return nil, err
	}
	return gojq.Compile(query)
}
