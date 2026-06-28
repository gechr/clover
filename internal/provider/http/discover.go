package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pattern"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
	"github.com/itchyny/gojq"
)

// maxSize caps the response download, so a mispointed url never streams a huge
// body into memory for jq or pattern matching.
const maxSize = 16 << 20 // 16 MiB

// Discover fetches the endpoint once and extracts candidate versions from the
// response. There is no pagination - a single response holds whatever the
// endpoint serves - so nothing is ever left unread.
func (p *Provider) Discover(ctx context.Context, r provider.Resource) ([]model.Candidate, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("http: invalid resource %T", r)
	}

	body, err := p.fetch(ctx, res.url, res.userAgent)
	if err != nil {
		return nil, err
	}

	var versions []string
	switch res.kind {
	case extractJQ:
		versions, err = jqVersions(res.jq, body)
	case extractPattern:
		versions = patternVersions(res.pattern, string(body))
	}
	if err != nil {
		return nil, err
	}
	return candidates(versions), nil
}

// fetch GETs url anonymously, identifying itself with userAgent, and reads up to
// maxSize bytes.
func (p *Provider) fetch(ctx context.Context, rawURL, userAgent string) ([]byte, error) {
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("http: build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: get %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		return nil, fmt.Errorf("http: get %s: %s", rawURL, resp.Status)
	}
	// Read one byte past the limit so an over-cap body is an error, never a
	// silently truncated prefix that could mis-parse.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("http: read %s: %w", rawURL, err)
	}
	if int64(len(data)) > maxSize {
		return nil, fmt.Errorf("http: %s exceeds the %d-byte limit", rawURL, maxSize)
	}
	return data, nil
}

// jqVersions runs the compiled jq program over the JSON body and collects every
// string it yields as a candidate version. The program may emit a stream (one
// value per Next, the .[].tag_name idiom) or a single array; either way each
// string is taken and a non-string result is ignored.
func jqVersions(code *gojq.Code, body []byte) ([]string, error) {
	var input any
	if err := json.Unmarshal(body, &input); err != nil {
		return nil, fmt.Errorf("http: %q: parse JSON response: %w", keyJQ, err)
	}

	var out []string
	iter := code.Run(input)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			return nil, fmt.Errorf("http: %q: %w", keyJQ, err)
		}
		out = append(out, jqStrings(v)...)
	}
	return out, nil
}

// jqStrings coerces one jq result into version strings: a string is taken as-is,
// an array contributes each of its string elements, anything else yields nothing.
func jqStrings(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// patternVersions runs the compiled pattern's unanchored regex over the body and
// takes the version-family capture from every match - the same anchoring
// findreplace uses to pick a line's version.
func patternVersions(p *pattern.Pattern, body string) []string {
	re := p.Regexp()
	g := versionGroup(p.Tokens())
	matches := re.FindAllStringSubmatch(body, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, pick(m, g))
	}
	return out
}

// versionGroup is the 1-based capture index of the first version-family token, or
// 0 when there is none (a /regex/, or a commit- or hex-only glob), in which case
// the candidate falls back to group 1 or the whole match. It mirrors
// match.versionGroup, kept local to avoid coupling the packages.
func versionGroup(tokens pattern.Tokens) int {
	for i, t := range tokens {
		//nolint:exhaustive // version-family subset; other tokens intentionally fall through.
		switch t {
		case pattern.TokenVersion,
			pattern.TokenMajor,
			pattern.TokenMinor,
			pattern.TokenPatch,
			pattern.TokenMajorMinor,
			pattern.TokenMajorMinorPatch:
			return i + 1
		}
	}
	return 0
}

// pick returns the version-family group when present, else group 1, else the
// whole match.
func pick(m []string, g int) string {
	if g > 0 && g < len(m) && m[g] != "" {
		return m[g]
	}
	if len(m) >= 2 && m[1] != "" {
		return m[1]
	}
	return m[0]
}

// candidates turns extracted version strings into model candidates,
// de-duplicated in first-seen order. A non-semver string keeps a nil Semver and
// is ordered out by selection, like any unparseable version elsewhere.
func candidates(versions []string) []model.Candidate {
	seen := make(map[string]bool, len(versions))
	out := make([]model.Candidate, 0, len(versions))
	for _, v := range versions {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		semver, _ := version.Parse(v)
		out = append(out, model.Candidate{Version: v, Semver: semver})
	}
	return out
}
