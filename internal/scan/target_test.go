package scan_test

import (
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/scan"
	"github.com/stretchr/testify/require"
)

func TestLocatedTarget(t *testing.T) {
	t.Parallel()

	lines := []string{
		"# clover: provider=docker repository=redis", // 0
		"metadata:",          // 1
		"  name: redis",      // 2
		"image: redis:7.2.0", // 3
		"image: nginx:1.25",  // 4
	}
	anchored := func(target string) directive.Directive {
		return directive.Directive{Pairs: []directive.KV{
			{Key: "provider", Value: "docker"},
			{Key: "target", Value: target},
		}}
	}
	pairs := func(kvs ...directive.KV) directive.Directive {
		return directive.Directive{Pairs: kvs}
	}

	tests := []struct {
		name    string
		loc     scan.Located
		want    int
		wantErr string
	}{
		{
			name: "no target key governs the next line",
			loc:  scan.Located{Line: 0, Directive: anchored("")},
			// an empty value still counts as the key being present
			wantErr: `"target" pattern is empty`,
		},
		{
			name: "absent target key governs the next line",
			loc: scan.Located{Line: 0, Directive: directive.Directive{
				Pairs: []directive.KV{{Key: "provider", Value: "docker"}},
			}},
			want: 1,
		},
		{
			name: "glob anchors to the first match below",
			loc:  scan.Located{Line: 0, Directive: anchored("image:*")},
			want: 3,
		},
		{
			name: "regex anchors to the first match below",
			loc:  scan.Located{Line: 0, Directive: anchored("/^image: nginx/")},
			want: 4,
		},
		{
			name: "search starts below the comment",
			loc:  scan.Located{Line: 3, Directive: anchored("image:*")},
			want: 4,
		},
		{
			name:    "no match below is an error",
			loc:     scan.Located{Line: 0, Directive: anchored("nonesuch*")},
			wantErr: `"target" matched no line below the comment`,
		},
		{
			name:    "an unmatchable pattern above the comment stays unmatched",
			loc:     scan.Located{Line: 4, Directive: anchored("image:*")},
			wantErr: `"target" matched no line below the comment`,
		},
		{
			name: "a bad regex reports the compile failure",
			loc:  scan.Located{Line: 0, Directive: anchored("/[/")},
			wantErr: `"target": compile regex pattern "/[/": ` +
				"error parsing regexp: missing closing ]: `[`",
		},
		{
			name: "a sidecar entry's line is already the target",
			loc:  scan.Located{Line: 3, Sidecar: true, Directive: anchored("image:*")},
			want: 3,
		},
		{
			name: "offset alone governs that many lines below",
			loc:  scan.Located{Line: 0, Directive: pairs(directive.KV{Key: "offset", Value: "3"})},
			want: 3,
		},
		{
			name: "the target search starts at the offset",
			loc: scan.Located{Line: 2, Directive: pairs(
				directive.KV{Key: "offset", Value: "2"},
				directive.KV{Key: "target", Value: "image:*"},
			)},
			want: 4, // offset skips the image match on line 3
		},
		{
			name: "a zero offset is rejected",
			loc: scan.Located{
				Line:      0,
				Directive: pairs(directive.KV{Key: "offset", Value: "0"}),
			},
			wantErr: `"offset" must be a positive integer`,
		},
		{
			name: "a non-numeric offset is rejected",
			loc: scan.Located{
				Line:      0,
				Directive: pairs(directive.KV{Key: "offset", Value: "two"}),
			},
			wantErr: `"offset" must be a positive integer`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.loc.Target(lines)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
