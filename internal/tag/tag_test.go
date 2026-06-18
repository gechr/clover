package tag_test

import (
	"testing"

	"github.com/gechr/clover/internal/tag"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		values  []string
		wantAll []string
		wantAny []string
		wantNot []string
		wantErr bool
	}{
		{name: "empty", values: nil},
		{name: "and via comma", values: []string{"prod,ci"}, wantAll: []string{"prod", "ci"}},
		{name: "or via slash", values: []string{"eu/us"}, wantAny: []string{"eu", "us"}},
		{name: "three-way or", values: []string{"a/b/c"}, wantAny: []string{"a", "b", "c"}},
		{name: "three-way and", values: []string{"a,b,c"}, wantAll: []string{"a", "b", "c"}},
		{name: "exclusion", values: []string{"!legacy"}, wantNot: []string{"legacy"}},
		{
			name:    "include and exclude mixed",
			values:  []string{"prod,!legacy"},
			wantAll: []string{"prod"},
			wantNot: []string{"legacy"},
		},
		{
			name:    "exclusion inside an or value",
			values:  []string{"eu/!legacy"},
			wantAny: []string{"eu"},
			wantNot: []string{"legacy"},
		},
		{name: "bare bang dropped", values: []string{"!"}},
		{name: "single bare value is AND", values: []string{"prod"}, wantAll: []string{"prod"}},
		{name: "parse does not dedup", values: []string{"a,a"}, wantAll: []string{"a", "a"}},
		{
			name:    "repeated flags accumulate",
			values:  []string{"prod,ci", "eu/us"},
			wantAll: []string{"prod", "ci"},
			wantAny: []string{"eu", "us"},
		},
		{
			name:    "whitespace trimmed, empties dropped",
			values:  []string{" prod , , ci "},
			wantAll: []string{"prod", "ci"},
		},
		{name: "mixed separators rejected", values: []string{"a,b/c"}, wantErr: true},
		{
			name:    "mixed separators with exclusion rejected",
			values:  []string{"eu/us,!legacy"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, err := tag.Parse(tc.values)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantAll, f.All)
			require.Equal(t, tc.wantAny, f.Any)
			require.Equal(t, tc.wantNot, f.Not)
		})
	}
}

func TestMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []string
		tags   []string
		want   bool
	}{
		{name: "empty filter matches everything", values: nil, tags: nil, want: true},
		{name: "empty filter matches untagged", values: nil, tags: []string{"prod"}, want: true},
		{
			name:   "AND satisfied",
			values: []string{"prod,ci"},
			tags:   []string{"prod", "ci", "eu"},
			want:   true,
		},
		{name: "AND missing one", values: []string{"prod,ci"}, tags: []string{"prod"}, want: false},
		{name: "OR satisfied", values: []string{"eu/us"}, tags: []string{"us"}, want: true},
		{name: "OR none", values: []string{"eu/us"}, tags: []string{"asia"}, want: false},
		{
			name:   "three-way OR matches last",
			values: []string{"a/b/c"},
			tags:   []string{"c"},
			want:   true,
		},
		{
			name:   "three-way OR matches none",
			values: []string{"a/b/c"},
			tags:   []string{"d"},
			want:   false,
		},
		{name: "untagged never matches a filter", values: []string{"prod"}, tags: nil, want: false},
		{name: "case-insensitive", values: []string{"Prod"}, tags: []string{"prod"}, want: true},
		{
			name:   "exclusion vetoes",
			values: []string{"!legacy"},
			tags:   []string{"legacy"},
			want:   false,
		},
		{
			name:   "exclusion keeps others",
			values: []string{"!legacy"},
			tags:   []string{"prod"},
			want:   true,
		},
		{name: "pure exclusion keeps untagged", values: []string{"!legacy"}, tags: nil, want: true},
		{
			name:   "include with exclusion",
			values: []string{"prod,!legacy"},
			tags:   []string{"prod", "legacy"},
			want:   false,
		},
		{
			name:   "include satisfied, not excluded",
			values: []string{"prod,!legacy"},
			tags:   []string{"prod"},
			want:   true,
		},
		{
			name:   "combined AND and OR both required",
			values: []string{"prod,ci", "eu/us"},
			tags:   []string{"prod", "ci", "us"},
			want:   true,
		},
		{
			name:   "combined fails when OR part missing",
			values: []string{"prod,ci", "eu/us"},
			tags:   []string{"prod", "ci"},
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, err := tag.Parse(tc.values)
			require.NoError(t, err)
			require.Equal(t, tc.want, f.Match(tc.tags))
		})
	}
}

func TestString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{name: "empty", values: nil, want: ""},
		{name: "single AND", values: []string{"prod"}, want: "prod"},
		{name: "AND", values: []string{"prod,ci"}, want: "prod AND ci"},
		{name: "OR", values: []string{"eu/us"}, want: "eu OR us"},
		{name: "pure exclusion", values: []string{"!legacy"}, want: "NOT legacy"},
		{
			name:   "include and exclusion",
			values: []string{"prod,!legacy"},
			want:   "prod AND NOT legacy",
		},
		{
			name:   "OR with exclusion parenthesised",
			values: []string{"eu/us", "!legacy"},
			want:   "(eu OR us) AND NOT legacy",
		},
		{
			name:   "combined parenthesised",
			values: []string{"prod,ci", "eu/us"},
			want:   "(prod AND ci) AND (eu OR us)",
		},
		{
			name:   "single each side unparenthesised",
			values: []string{"prod", "eu"},
			want:   "prod AND eu",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, err := tag.Parse(tc.values)
			require.NoError(t, err)
			require.Equal(t, tc.want, f.String())
		})
	}
}
