package httpcache

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFresh(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tests := map[string]struct {
		entry Entry
		want  bool
	}{
		"within max-age": {
			entry: Entry{
				Header:   http.Header{"Cache-Control": {"public, max-age=60"}},
				StoredAt: now.Add(-30 * time.Second),
			},
			want: true,
		},
		"past max-age": {
			entry: Entry{
				Header:   http.Header{"Cache-Control": {"public, max-age=60"}},
				StoredAt: now.Add(-90 * time.Second),
			},
			want: false,
		},
		"no freshness info": {
			entry: Entry{
				Header:   http.Header{"Etag": {`W/"abc"`}},
				StoredAt: now,
			},
			want: false,
		},
		"zero max-age": {
			entry: Entry{
				Header:   http.Header{"Cache-Control": {"max-age=0"}},
				StoredAt: now,
			},
			want: false,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tt.entry.fresh(now))
		})
	}
}

func TestRevalidatable(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		header http.Header
		want   bool
	}{
		"etag": {header: http.Header{"Etag": {`W/"abc"`}}, want: true},
		"last-modified": {
			header: http.Header{"Last-Modified": {"Wed, 24 Jun 2026 23:36:26 GMT"}},
			want:   true,
		},
		"neither": {header: http.Header{"Cache-Control": {"max-age=60"}}, want: false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			entry := Entry{Header: tt.header}
			require.Equal(t, tt.want, entry.revalidatable())
		})
	}
}

func TestLifetime(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		header http.Header
		want   time.Duration
		ok     bool
	}{
		"max-age": {
			header: http.Header{"Cache-Control": {"public, max-age=60"}},
			want:   time.Minute,
			ok:     true,
		},
		"max-age case-insensitive": {
			header: http.Header{"Cache-Control": {"Max-Age=5"}},
			want:   5 * time.Second,
			ok:     true,
		},
		"expires minus date": {
			header: http.Header{
				"Date":    {"Wed, 24 Jun 2026 23:00:00 GMT"},
				"Expires": {"Wed, 24 Jun 2026 23:10:00 GMT"},
			},
			want: 10 * time.Minute,
			ok:   true,
		},
		"max-age wins over expires": {
			header: http.Header{
				"Cache-Control": {"max-age=60"},
				"Date":          {"Wed, 24 Jun 2026 23:00:00 GMT"},
				"Expires":       {"Wed, 24 Jun 2026 23:10:00 GMT"},
			},
			want: time.Minute,
			ok:   true,
		},
		"expires without date": {
			header: http.Header{"Expires": {"Wed, 24 Jun 2026 23:10:00 GMT"}},
		},
		"expires in the past": {
			header: http.Header{
				"Date":    {"Wed, 24 Jun 2026 23:00:00 GMT"},
				"Expires": {"Wed, 24 Jun 2026 22:00:00 GMT"},
			},
		},
		"malformed max-age": {
			header: http.Header{"Cache-Control": {"max-age=soon"}},
		},
		"negative max-age": {
			header: http.Header{"Cache-Control": {"max-age=-5"}},
		},
		"no headers": {
			header: http.Header{},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := lifetime(tt.header)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}
