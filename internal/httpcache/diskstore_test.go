package httpcache_test

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gechr/clover/internal/httpcache"
	"github.com/stretchr/testify/require"
)

var storedAt = time.Date(2026, 6, 24, 23, 36, 26, 0, time.UTC)

func diskStore(t *testing.T) (*httpcache.DiskStore, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := httpcache.NewDiskStore(dir)
	require.NoError(t, err)
	return store, dir
}

func TestDiskStoreRoundTrip(t *testing.T) {
	t.Parallel()

	store, _ := diskStore(t)
	entry := &httpcache.Entry{
		Status:   http.StatusOK,
		Header:   http.Header{"Etag": {`W/"v1"`}, "Content-Type": {"application/json"}},
		Body:     []byte("body"),
		StoredAt: storedAt,
	}
	store.Set("key", entry)

	got, ok := store.Get("key")
	require.True(t, ok)
	require.Equal(t, entry, got)
}

func TestDiskStoreMissingKeyIsMiss(t *testing.T) {
	t.Parallel()

	store, _ := diskStore(t)
	_, ok := store.Get("absent")
	require.False(t, ok)
}

func TestDiskStoreCorruptFileRemoved(t *testing.T) {
	t.Parallel()

	store, dir := diskStore(t)
	path := filepath.Join(dir, "key")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))

	_, ok := store.Get("key")
	require.False(t, ok)
	require.NoFileExists(t, path)
}

func TestDiskStoreFilePermissions(t *testing.T) {
	t.Parallel()

	store, dir := diskStore(t)
	store.Set("key", &httpcache.Entry{
		Status:   http.StatusOK,
		Header:   http.Header{"Etag": {`W/"v1"`}},
		StoredAt: storedAt,
	})

	info, err := os.Stat(filepath.Join(dir, "key"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestDiskStoreAdmission(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		header http.Header
		want   bool
	}{
		"etag": {header: http.Header{"Etag": {`W/"v1"`}}, want: true},
		"last-modified": {
			header: http.Header{"Last-Modified": {"Wed, 24 Jun 2026 23:36:26 GMT"}},
			want:   true,
		},
		"max-age only": {
			header: http.Header{"Cache-Control": {"max-age=60"}},
			want:   true,
		},
		"no validator or freshness": {
			header: http.Header{"Content-Type": {"application/json"}},
			want:   false,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store, dir := diskStore(t)
			store.Set("key", &httpcache.Entry{
				Status:   http.StatusOK,
				Header:   tt.header,
				StoredAt: storedAt,
			})
			_, err := os.Stat(filepath.Join(dir, "key"))
			require.Equal(t, tt.want, err == nil)
		})
	}
}

func TestDiskStoreStripsSensitiveHeaders(t *testing.T) {
	t.Parallel()

	store, _ := diskStore(t)
	store.Set("key", &httpcache.Entry{
		Status: http.StatusOK,
		Header: http.Header{
			"Etag":               {`W/"v1"`},
			"Set-Cookie":         {"session=secret"},
			"Authorization":      {"Bearer token"},
			"Proxy-Authenticate": {"Basic"},
		},
		StoredAt: storedAt,
	})

	got, ok := store.Get("key")
	require.True(t, ok)
	require.Equal(t, http.Header{"Etag": {`W/"v1"`}}, got.Header)
}

func TestDiskStoreGCRemovesOldEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	old := filepath.Join(dir, "old")
	recent := filepath.Join(dir, "recent")
	require.NoError(t, os.WriteFile(old, []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(recent, []byte("{}"), 0o600))
	stale := time.Now().Add(-31 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(old, stale, stale))

	_, err := httpcache.NewDiskStore(dir)
	require.NoError(t, err)
	require.NoFileExists(t, old)
	require.FileExists(t, recent)
}

func TestLayeredStorePromotesDiskHits(t *testing.T) {
	t.Parallel()

	mem := httpcache.NewMemStore()
	disk := httpcache.NewMemStore()
	layered := httpcache.NewLayeredStore(mem, disk)
	entry := &httpcache.Entry{Status: http.StatusOK, Header: http.Header{}, StoredAt: storedAt}
	disk.Set("key", entry)

	got, ok := layered.Get("key")
	require.True(t, ok)
	require.Equal(t, entry, got)
	promoted, ok := mem.Get("key")
	require.True(t, ok)
	require.Equal(t, entry, promoted)
}

func TestLayeredStoreSetWritesBothLayers(t *testing.T) {
	t.Parallel()

	mem := httpcache.NewMemStore()
	disk := httpcache.NewMemStore()
	layered := httpcache.NewLayeredStore(mem, disk)
	entry := &httpcache.Entry{Status: http.StatusOK, Header: http.Header{}, StoredAt: storedAt}
	layered.Set("key", entry)

	_, ok := mem.Get("key")
	require.True(t, ok)
	_, ok = disk.Get("key")
	require.True(t, ok)
}

// TestEnableSharedDisk is deliberately not parallel: it toggles the process-wide
// store, so it must never overlap tests relying on the in-memory default.
func TestEnableSharedDisk(t *testing.T) { //nolint:paralleltest // mutates the shared store
	dir := t.TempDir()
	require.NoError(t, httpcache.EnableSharedDisk(dir))
	t.Cleanup(httpcache.ResetSharedDisk)

	fake := &fakeTransport{body: "cached", header: http.Header{"Etag": {`W/"v1"`}}}
	client := httpcache.New(httpcache.WithTransport(fake))
	require.Equal(t, "cached", get(t, client, "https://example.test/shared"))

	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 1, "the default store persists cacheable entries to disk")
}

func TestEnableSharedDiskUnusableDir(
	t *testing.T,
) { //nolint:paralleltest // mutates the shared store
	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))
	require.Error(t, httpcache.EnableSharedDisk(filepath.Join(blocker, "nested")))
}

// TestDiskStoreAcrossClients is the cross-run scenario end to end: a second
// client with a fresh in-memory tier revalidates the disk entry with a
// conditional request and serves the persisted body on 304.
func TestDiskStoreAcrossClients(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	disk1, err := httpcache.NewDiskStore(dir)
	require.NoError(t, err)
	fake1 := &fakeTransport{body: "cached", header: http.Header{"Etag": {`W/"v1"`}}}
	client1 := httpcache.New(
		httpcache.WithStore(httpcache.NewLayeredStore(httpcache.NewMemStore(), disk1)),
		httpcache.WithTransport(fake1),
	)
	require.Equal(t, "cached", get(t, client1, "https://example.test/a"))
	time.Sleep(10 * time.Millisecond)

	disk2, err := httpcache.NewDiskStore(dir)
	require.NoError(t, err)
	var conditional []*http.Request
	fake2 := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		conditional = append(conditional, req)
		return &http.Response{
			StatusCode: http.StatusNotModified,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	}}
	client2 := httpcache.New(
		httpcache.WithStore(httpcache.NewLayeredStore(httpcache.NewMemStore(), disk2)),
		httpcache.WithTransport(fake2),
	)

	require.Equal(t, "cached", get(t, client2, "https://example.test/a"))
	require.Len(t, conditional, 1)
	require.Equal(t, `W/"v1"`, conditional[0].Header.Get("If-None-Match"))
}
