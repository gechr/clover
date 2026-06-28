// Package http resolves versions from an arbitrary HTTP endpoint, the escape
// hatch for any source clover has no bespoke provider for. It fetches a
// user-supplied url once, anonymously, and extracts one-or-many version strings
// from the response - a jq program over a JSON body, or a glob/regex over a text
// body - leaving the framework to own selection. The extraction expression is
// compiled at validation time, so clover lint catches a malformed jq or pattern
// offline before any fetch.
package http
