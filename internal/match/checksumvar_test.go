package match_test

import (
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/stretchr/testify/require"
)

// sum is a 64-character hex value, the shape the checksum routes gate on.
const sum = "0000000000000000000000000000000000000000000000000000000000000000"

// goDownload names the asset a GO_VERSION pin's sum belongs to, in the spelling a
// Dockerfile fetches it with.
const goDownload = "RUN curl -fsSLO https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"

func TestInferChecksumVariable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		path  string
		lines []string
		// target is the line the inference is taken for; the checksum half unless a
		// case is about the producer earning its id.
		target int
		want   match.Inference
		ok     bool
	}{
		{
			name: "a sum paired with its version variable follows it",
			path: "Dockerfile",
			lines: []string{
				"ARG GO_VERSION=1.24.0",
				"ARG GO_SHA256=" + sum,
				goDownload,
			},
			target: 1,
			want: match.Inference{
				Provider: "follow",
				From:     "go",
				Value:    "sha256",
				Pattern:  "go<version>.linux-amd64.tar.gz",
			},
			ok: true,
		},
		{
			// The producer half of the same pairing: it earns an id only because a
			// follower needs one, so the two are inferred from one pairing.
			name: "the version half publishes the id its follower names",
			path: "Dockerfile",
			lines: []string{
				"ARG GO_VERSION=1.24.0",
				"ARG GO_SHA256=" + sum,
				goDownload,
			},
			target: 0,
			want:   match.Inference{Provider: "go", ID: "go"},
			ok:     true,
		},
		{
			name: "every suffix spelling is claimed",
			path: "Dockerfile",
			lines: []string{
				"ARG GO_VERSION=1.24.0",
				"ARG GO_SHA256SUM=" + sum,
				goDownload,
			},
			target: 1,
			want: match.Inference{
				Provider: "follow",
				From:     "go",
				Value:    "sha256",
				Pattern:  "go<version>.linux-amd64.tar.gz",
			},
			ok: true,
		},
		{
			name: "a checksum spelling of the same pair",
			path: "Dockerfile",
			lines: []string{
				"ENV SOPS_VERSION=3.9.0",
				"ENV SOPS_CHECKSUM=" + sum,
				"RUN curl -fsSLO https://github.com/getsops/sops/releases/download/" +
					"v${SOPS_VERSION}/sops-v${SOPS_VERSION}.linux.amd64",
			},
			target: 1,
			want: match.Inference{
				Provider: "follow",
				From:     "sops",
				Value:    "sha256",
				Pattern:  "sops-v<version>.linux.amd64",
			},
			ok: true,
		},
		{
			// A workflow reaches an env: value through an expression, whose spaces sit
			// inside the reference - so the substitution has to happen before the line
			// is split into fields.
			name: "a workflow env pair reads an actions expression",
			path: ".github/workflows/ci.yml",
			lines: []string{
				"env:",
				"  GO_VERSION: '1.24.0'",
				"  GO_SHA256: '" + sum + "'",
				"    run: curl -fsSLO https://go.dev/dl/go${{ env.GO_VERSION }}.linux-amd64.tar.gz",
			},
			target: 2,
			want: match.Inference{
				Provider: "follow",
				From:     "go",
				Value:    "sha256",
				Pattern:  "go<version>.linux-amd64.tar.gz",
			},
			ok: true,
		},
		{
			// The multi-arch spelling: the sum on the line belongs to one platform and
			// the file does not say which, so pinning either is a coin flip.
			name: "an arch variable in the asset name declines",
			path: "Dockerfile",
			lines: []string{
				"ARG GO_VERSION=1.24.0",
				"ARG GO_SHA256=" + sum,
				"RUN curl -fsSLO https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz",
			},
			target: 1,
			ok:     false,
		},
		{
			name: "a sum whose asset the file never names declines",
			path: "Dockerfile",
			lines: []string{
				"ARG GO_VERSION=1.24.0",
				"ARG GO_SHA256=" + sum,
			},
			target: 1,
			ok:     false,
		},
		{
			// Only a URL names the asset; a path the version merely appears in does
			// not, or `mkdir -p /opt/go$GO_VERSION` would supply a filename.
			name: "a non-URL mention of the variable is not an asset",
			path: "Dockerfile",
			lines: []string{
				"ARG GO_VERSION=1.24.0",
				"ARG GO_SHA256=" + sum,
				"RUN mkdir -p /opt/go${GO_VERSION}",
			},
			target: 1,
			ok:     false,
		},
		{
			name: "two downloads disagreeing about the asset declines",
			path: "Dockerfile",
			lines: []string{
				"ARG GO_VERSION=1.24.0",
				"ARG GO_SHA256=" + sum,
				goDownload,
				"RUN curl -fsSLO https://go.dev/dl/go${GO_VERSION}.linux-arm64.tar.gz",
			},
			target: 1,
			ok:     false,
		},
		{
			// A query and a fragment follow the path, so neither is part of the
			// filename - and `?` is a glob metacharacter that would match where it was
			// never meant to.
			name: "a query string and fragment are not part of the asset",
			path: "Dockerfile",
			lines: []string{
				"ARG GO_VERSION=1.24.0",
				"ARG GO_SHA256=" + sum,
				"RUN curl -fsSLO https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz?token=x#main",
			},
			target: 1,
			want: match.Inference{
				Provider: "follow",
				From:     "go",
				Value:    "sha256",
				Pattern:  "go<version>.linux-amd64.tar.gz",
			},
			ok: true,
		},
		{
			// Cutting the query can leave a basename with no version in it, which is
			// the right answer for a URL whose filename is not in its path: there is no
			// asset name to pattern against.
			name: "an asset named inside a query string declines",
			path: "Dockerfile",
			lines: []string{
				"ARG GO_VERSION=1.24.0",
				"ARG GO_SHA256=" + sum,
				"RUN curl -fsSLO https://example.com/download?file=go${GO_VERSION}.tar.gz",
			},
			target: 1,
			ok:     false,
		},
		{
			// A repeated prefix cannot say which sum belongs to which pin.
			name: "a repeated checksum prefix is ambiguous",
			path: "Dockerfile",
			lines: []string{
				"ARG GO_VERSION=1.24.0",
				"ARG GO_SHA256=" + sum,
				"ARG GO_SHA256SUM=" + sum,
				goDownload,
			},
			target: 1,
			ok:     false,
		},
		{
			// The platform-suffixed spelling a multi-arch build uses: go-amd64 places
			// no tool, so the prefix rule declines it without needing a platform rule.
			name: "a platform-suffixed prefix places no tool",
			path: "Dockerfile",
			lines: []string{
				"ARG GO_VERSION=1.24.0",
				"ARG GO_AMD64_SHA256=" + sum,
				goDownload,
			},
			target: 1,
			ok:     false,
		},
		{
			// _SHA is accepted as a spelling, but a 40-character commit is not a sum,
			// so the value gate declines it rather than the name.
			name: "a commit-length hash is not a sum",
			path: "Dockerfile",
			lines: []string{
				"ARG GO_VERSION=1.24.0",
				"ARG GO_SHA=e5c1b9a4f2d3c8a7b6e0f1d2c3b4a5968778695a",
				goDownload,
			},
			target: 1,
			ok:     false,
		},
		{
			name: "a sum whose prefix places no tool declines",
			path: "Dockerfile",
			lines: []string{
				"ARG APP_VERSION=2.1.0",
				"ARG APP_SHA256=" + sum,
				"RUN curl -fsSLO https://example.com/app-${APP_VERSION}.tar.gz",
			},
			target: 1,
			ok:     false,
		},
		{
			// The prefix is never read segment by segment, on the same terms as the
			// version half: this is a service's API sum, not a Node.js one.
			name: "a trailing segment naming a tool is not a tool",
			path: "Dockerfile",
			lines: []string{
				"ARG API_NODE_VERSION=18.0.0",
				"ARG API_NODE_SHA256=" + sum,
				"RUN curl -fsSLO https://example.com/api-${API_NODE_VERSION}.tgz",
			},
			target: 1,
			ok:     false,
		},
		{
			name: "an unindented workflow key is not a variable",
			path: ".github/workflows/ci.yml",
			lines: []string{
				"GO_VERSION: '1.24.0'",
				"GO_SHA256: '" + sum + "'",
				"    run: " + goDownload,
			},
			target: 1,
			ok:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := match.Infer(tt.path, tt.lines, tt.target)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

// A version variable with no checksum sibling publishes no id, so an ordinary pin
// still earns the bare `@clover` shorthand rather than a gratuitous producer name.
func TestVersionVariableWithoutChecksumPublishesNoID(t *testing.T) {
	t.Parallel()

	got, ok := match.Infer("Dockerfile", []string{"ARG GO_VERSION=1.24.0"}, 0)
	require.True(t, ok)
	require.Equal(t, match.Inference{Provider: "go"}, got)
}

// The rewriter a checksum-variable line dispatches, which is the one thing the
// two new routes decide for a marker that is not a follower.
//
// A follower carries value=sha256 and is claimed by the value-gated routes, which
// dispatch the hash rewriter. Everything else reaching a sum line is an explicit
// provider aimed at it, always a mistake - and the smart rewriter locating no
// version there is the desired outcome. The hash rewriter would locate the hex
// and splice the resolved version over it, so the mistake would write a version
// into a checksum field instead of failing.
func TestChecksumVariableRewriterDispatch(t *testing.T) {
	t.Parallel()

	const line = "ARG GO_SHA256=" + sum

	t.Run("an explicit provider locates no version", func(t *testing.T) {
		t.Parallel()

		rewriter := match.For(match.Context{
			Path:     "Dockerfile",
			Line:     line,
			Provider: "github",
		})
		require.IsType(t, match.Smart{}, rewriter)
		_, err := rewriter.Locate(line)
		require.EqualError(t, err, "no version found on target line")
	})

	t.Run("a follower locates the sum", func(t *testing.T) {
		t.Parallel()

		rewriter := match.For(match.Context{
			Path:  "Dockerfile",
			Line:  line,
			Value: "sha256",
		})
		require.IsType(t, match.Hash{}, rewriter)
		located, err := rewriter.Locate(line)
		require.NoError(t, err)
		require.Equal(t, sum, located.Current())
	})
}

// The pairing is refused inside a build matrix like every other pin there: a
// matrix lists the versions a project supports, so its entries are deliberate.
func TestChecksumVariableRefusedInMatrix(t *testing.T) {
	t.Parallel()

	lines := []string{
		"strategy:",
		"  matrix:",
		"    GO_VERSION: '1.24.0'",
		"    GO_SHA256: '" + sum + "'",
		"    run: " + goDownload,
	}
	_, ok := match.Infer(".github/workflows/ci.yml", lines, 3)
	require.False(t, ok)
}
