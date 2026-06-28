// Package checksum sources a sha256 for a value=sha256 follower. The source is
// selectable (digest, checksums file, or download-and-hash) with an auto chain
// and a verify cross-check; see [Resolve].
package checksum

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gechr/clover/internal/pattern"
	xstrings "github.com/gechr/x/strings"
)

// maxSize caps the checksum-file download, so a mispointed URL never streams a
// huge body.
const maxSize = 1 << 20 // 1 MiB

// entry is one parsed checksum line: a hash and the asset filename it is for
// (empty for a bare single-hash file).
type entry struct {
	hash string
	file string
}

// Fetch downloads the checksum file at rawURL (with <version> expanded) and
// returns the sha256 for the asset matching pat. An empty pat is allowed only
// when the file holds a single entry.
func Fetch(ctx context.Context, client *http.Client, rawURL, version, pat string) (string, error) {
	//nolint:exhaustive // a substitution map supplies only the tokens it has a value for.
	url := pattern.Expand(rawURL, pattern.TokenMap{pattern.TokenVersion: version})
	data, err := fetchBody(ctx, client, url, maxSize)
	if err != nil {
		return "", err
	}
	entries := parse(string(data))
	if len(entries) == 0 {
		return "", fmt.Errorf("checksum: no sha256 entries at %s", url)
	}
	return choose(entries, pat)
}

// fetchBody GETs url and reads up to limit bytes.
func fetchBody(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("checksum: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("checksum: get %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("checksum: get %s: %s", url, resp.Status)
	}
	// Read one byte past the limit so an over-cap file is an error, never a
	// silently truncated prefix that could mis-parse.
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("checksum: read %s: %w", url, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("checksum: %s exceeds the %d-byte limit", url, limit)
	}
	return data, nil
}

// parse reads checksum lines in the common shapes - "<hash>  <file>" (with an
// optional binary-mode "*" before the file), "<file> <hash>", or a bare
// "<hash>" - keeping only sha256-shaped hashes.
func parse(data string) []entry {
	var entries []entry
	for line := range strings.Lines(data) {
		if e, ok := parseLine(strings.Fields(line)); ok {
			entries = append(entries, e)
		}
	}
	return entries
}

// parseLine finds the sha256 token among a line's fields and treats any other
// field as its filename (stripping a binary-mode "*").
func parseLine(fields []string) (entry, bool) {
	for i, f := range fields {
		if !xstrings.IsSHA256(f) {
			continue
		}
		e := entry{hash: f}
		for j, other := range fields {
			if j != i {
				e.file = strings.TrimPrefix(other, "*")
				break
			}
		}
		return e, true
	}
	return entry{}, false
}

// choose returns the hash for the entry matching pat, or the sole entry when pat
// is empty. Anything ambiguous is an error.
func choose(entries []entry, pat string) (string, error) {
	if pat == "" {
		if len(entries) == 1 {
			return entries[0].hash, nil
		}
		return "", fmt.Errorf("checksum: %d entries, set %q to choose one", len(entries), "pattern")
	}

	p, err := pattern.Compile(pat)
	if err != nil {
		return "", fmt.Errorf("checksum: invalid pattern %q: %w", pat, err)
	}
	var matched []string
	for _, e := range entries {
		if p.Matches(e.file) {
			matched = append(matched, e.hash)
		}
	}
	switch len(matched) {
	case 1:
		return matched[0], nil
	case 0:
		return "", fmt.Errorf("checksum: no asset matched pattern %q", pat)
	default:
		return "", fmt.Errorf("checksum: pattern %q matched %d assets", pat, len(matched))
	}
}
