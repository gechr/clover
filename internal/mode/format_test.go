package mode_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// orderedProvider declares two keys so format's provider-block ordering can be
// exercised. It never resolves - format is offline.
type orderedProvider struct{ name string }

func (p orderedProvider) Name() string { return p.name }

func (p orderedProvider) Keys() []provider.Key {
	return []provider.Key{{Name: "repository", Required: true}, {Name: "source"}}
}

func (p orderedProvider) Resource(directive.Directive) (provider.Resource, error) {
	return p.name, nil
}

func (p orderedProvider) Describe(provider.Resource) string { return p.name }

func (p orderedProvider) Discover(context.Context, provider.Resource) ([]model.Candidate, error) {
	panic("format must never call Discover")
}

// formatDir writes a single file and returns the dir and the file path.
func formatDir(t *testing.T, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.txt")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return dir, path
}

// formatRepo writes a repository root (a .git marker, a .clover.yaml, and one
// named directive file) and returns the root and that file's path. It exercises
// the per-root config wiring Format applies through its own resolver.
func formatRepo(t *testing.T, cloverYAML, name, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".clover.yaml"), []byte(cloverYAML), 0o644))
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return dir, path
}

func TestFormatHonorsPerRootExclude(t *testing.T) {
	provider.Register(orderedProvider{name: "fmtexcl"})
	// A file under the root's paths.exclude is skipped by the scan, so format
	// never reformats it - the resolver Format wires in supplies the glob.
	original := "# clover: source=tags repository=a/b provider=fmtexcl\nversion: 1.0.0\n"
	dir, path := formatRepo(
		t,
		"paths:\n  exclude:\n    - vendor/**\n",
		"vendor/app.txt",
		original,
	)

	summary, err := mode.Format(
		context.Background(),
		[]string{dir},
		false,
		nil,
		config.NewResolver(nil, "", false),
		testWorkers,
	)
	require.NoError(t, err)
	require.True(t, summary.OK(), "the excluded file yields no changes")

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, string(got), "an excluded file is left untouched")
}

func TestFormatSkipsUnsatisfiedRequiredVersion(t *testing.T) {
	provider.Register(orderedProvider{name: "fmtreq"})
	// The repo's required-version the running clover does not satisfy is skipped,
	// so its directive file is dropped before formatting.
	original := "# clover: source=tags repository=a/b provider=fmtreq\nversion: 1.0.0\n"
	dir, path := formatRepo(t, "required-version: \">=9.0.0\"\n", "app.txt", original)

	summary, err := mode.Format(
		context.Background(),
		[]string{dir},
		false,
		nil,
		config.NewResolver(nil, "", false),
		testWorkers,
		pipeline.WithVersion("1.0.0"),
	)
	require.NoError(t, err)
	require.True(t, summary.OK(), "the skipped repository yields no changes")

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, string(got), "a skipped repo's file is left untouched")
}

func TestFormatReordersAndWrites(t *testing.T) {
	provider.Register(orderedProvider{name: "fmtp"})
	dir, path := formatDir(
		t,
		"# clover: source=tags constraint=patch repository=a/b provider=fmtp\nversion: 1.0.0\n",
	)

	summary, err := mode.Format(
		context.Background(),
		[]string{dir},
		false,
		nil,
		config.NewResolver(nil, "", false),
		testWorkers,
	)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Changed())
	require.True(t, summary.Files[0].Written)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t,
		"# clover: provider=fmtp repository=a/b source=tags constraint=patch\nversion: 1.0.0\n",
		string(got),
	)
}

func TestFormatLowercasesAndUniquesTags(t *testing.T) {
	provider.Register(orderedProvider{name: "fmttag"})
	dir, path := formatDir(
		t,
		"# clover:  provider=fmttag   repository=a/b  tags=PROD,Ci,prod\nversion: 1.0.0\n",
	)

	summary, err := mode.Format(
		context.Background(),
		[]string{dir},
		false,
		nil,
		config.NewResolver(nil, "", false),
		testWorkers,
	)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Changed())

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t,
		"# clover: provider=fmttag repository=a/b tags=prod,ci\nversion: 1.0.0\n",
		string(got),
		"tags lowercased and de-duplicated, and spacing collapsed to one space per pair",
	)
}

func TestFormatLeavesVersionLineUntouched(t *testing.T) {
	provider.Register(orderedProvider{name: "fmtv"})
	dir, path := formatDir(t, "# clover: repository=a/b provider=fmtv\nversion: 1.2.3-rc.1\n")

	_, err := mode.Format(
		context.Background(),
		[]string{dir},
		false,
		nil,
		config.NewResolver(nil, "", false),
		testWorkers,
	)
	require.NoError(t, err)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "# clover: provider=fmtv repository=a/b\nversion: 1.2.3-rc.1\n", string(got))
}

func TestFormatCheckWritesNothing(t *testing.T) {
	provider.Register(orderedProvider{name: "fmtc"})
	original := "# clover: repository=a/b provider=fmtc\nversion: 1.0.0\n"
	dir, path := formatDir(t, original)

	summary, err := mode.Format(
		context.Background(),
		[]string{dir},
		true,
		nil,
		config.NewResolver(nil, "", false),
		testWorkers,
	)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Changed())
	require.False(t, summary.OK())
	require.False(t, summary.Files[0].Written)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, string(got)) // check never writes
}

func TestFormatAlreadyCanonicalIsNoop(t *testing.T) {
	provider.Register(orderedProvider{name: "fmtn"})
	original := "# clover: provider=fmtn repository=a/b\nversion: 1.0.0\n"
	dir, path := formatDir(t, original)

	summary, err := mode.Format(
		context.Background(),
		[]string{dir},
		false,
		nil,
		config.NewResolver(nil, "", false),
		testWorkers,
	)
	require.NoError(t, err)
	require.True(t, summary.OK())
	require.False(t, summary.Files[0].Written)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, string(got))
}

func TestFormatIsIdempotent(t *testing.T) {
	provider.Register(orderedProvider{name: "fmti"})
	dir, path := formatDir(t, "# clover: source=tags repository=a/b provider=fmti\nv: 1.0.0\n")

	_, err := mode.Format(
		context.Background(),
		[]string{dir},
		false,
		nil,
		config.NewResolver(nil, "", false),
		testWorkers,
	)
	require.NoError(t, err)
	first, err := os.ReadFile(path)
	require.NoError(t, err)

	summary, err := mode.Format(
		context.Background(),
		[]string{dir},
		false,
		nil,
		config.NewResolver(nil, "", false),
		testWorkers,
	)
	require.NoError(t, err)
	require.True(t, summary.OK()) // second pass finds nothing to change
	second, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(first), string(second))
}

func TestFormatNormalisesQuoting(t *testing.T) {
	provider.Register(orderedProvider{name: "fmtq"})
	dir, path := formatDir(t, `# clover: provider=fmtq repository="a/b"`+"\nv: 1.0.0\n")

	_, err := mode.Format(
		context.Background(),
		[]string{dir},
		false,
		nil,
		config.NewResolver(nil, "", false),
		testWorkers,
	)
	require.NoError(t, err)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "# clover: provider=fmtq repository=a/b\nv: 1.0.0\n", string(got))
}

func TestFormatPreservesBlockComment(t *testing.T) {
	provider.Register(orderedProvider{name: "fmtb"})
	dir := t.TempDir()
	path := filepath.Join(dir, "index.html")
	require.NoError(
		t,
		os.WriteFile(
			path,
			[]byte("<!-- clover: repository=a/b provider=fmtb -->\nv: 1.0.0\n"),
			0o644,
		),
	)

	_, err := mode.Format(
		context.Background(),
		[]string{dir},
		false,
		nil,
		config.NewResolver(nil, "", false),
		testWorkers,
	)
	require.NoError(t, err)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "<!-- clover: provider=fmtb repository=a/b -->\nv: 1.0.0\n", string(got))
}

func TestFormatPreservesFileMode(t *testing.T) {
	provider.Register(orderedProvider{name: "fmtperm"})
	dir, path := formatDir(t, "# clover: repository=a/b provider=fmtperm\nv: 1.0.0\n")
	require.NoError(t, os.Chmod(path, 0o777))

	_, err := mode.Format(
		context.Background(),
		[]string{dir},
		false,
		nil,
		config.NewResolver(nil, "", false),
		testWorkers,
	)
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o777), info.Mode().Perm()) // clover never changes perms
}

func TestFormatRejectsUnknownKey(t *testing.T) {
	provider.Register(orderedProvider{name: "fmtuk"})
	original := "# clover: provider=fmtuk repository=a/b max-major=4\nversion: 1.0.0\n"
	dir, path := formatDir(t, original)

	summary, err := mode.Format(
		context.Background(),
		[]string{dir},
		false,
		nil,
		config.NewResolver(nil, "", false),
		testWorkers,
	)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Errored())
	require.Equal(t, 0, summary.Changed(), "a directive with an unknown key is left untouched")
	require.False(t, summary.OK())

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, string(got), "the rejected line is not rewritten")
}

func TestFormatPruneRemovesUnknownKey(t *testing.T) {
	provider.Register(orderedProvider{name: "fmtpr"})
	dir, path := formatDir(
		t,
		"# clover: provider=fmtpr repository=a/b max-major=4 constraint=minor\nversion: 1.0.0\n",
	)

	summary, err := mode.Format(
		context.Background(),
		[]string{dir},
		false,
		new(true),
		config.NewResolver(nil, "", false),
		testWorkers,
	)
	require.NoError(t, err)
	require.Equal(t, 0, summary.Errored(), "prune removes the key rather than erroring")
	require.Equal(t, 1, summary.Changed())
	require.Equal(t, []string{"max-major"}, summary.Files[0].Changes[0].Pruned)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t,
		"# clover: provider=fmtpr repository=a/b constraint=minor\nversion: 1.0.0\n",
		string(got),
		"the unknown key is stripped, known keys preserved and reordered",
	)
}

func TestFormatFollowerReordersCommonKeys(t *testing.T) {
	// No provider= ⇒ a follower; only common keys, reordered (from before value).
	dir, path := formatDir(t, "# clover: value=version from=app\nv: 1.0.0\n")

	_, err := mode.Format(
		context.Background(),
		[]string{dir},
		false,
		nil,
		config.NewResolver(nil, "", false),
		testWorkers,
	)
	require.NoError(t, err)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "# clover: from=app value=version\nv: 1.0.0\n", string(got))
}
