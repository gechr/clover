package directive_test

import (
	"testing"
	"time"

	"github.com/gechr/cusp/internal/directive"
	"github.com/stretchr/testify/require"
)

func newDirective(pairs ...directive.KV) directive.Directive {
	return directive.Directive{Pairs: pairs}
}

func TestDirectiveGet(t *testing.T) {
	t.Parallel()

	d := newDirective(
		directive.KV{Key: "include", Value: "a"},
		directive.KV{Key: "include", Value: "b"},
		directive.KV{Key: "provider", Value: "github"},
	)

	got, ok := d.Get("provider")
	require.True(t, ok)
	require.Equal(t, "github", got)

	// Get returns the first occurrence of a repeated key.
	got, ok = d.Get("include")
	require.True(t, ok)
	require.Equal(t, "a", got)

	_, ok = d.Get("missing")
	require.False(t, ok)
}

func TestDirectiveAll(t *testing.T) {
	t.Parallel()

	d := newDirective(
		directive.KV{Key: "include", Value: "a"},
		directive.KV{Key: "exclude", Value: "x"},
		directive.KV{Key: "include", Value: "b"},
	)

	require.Equal(t, []string{"a", "b"}, d.All("include"))
	require.Nil(t, d.All("missing"))
}

func TestDirectiveHas(t *testing.T) {
	t.Parallel()

	d := newDirective(directive.KV{Key: "skip", Value: "true"})

	require.True(t, d.Has("skip"))
	require.False(t, d.Has("force"))
}

func TestDirectiveBool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kv      directive.KV
		key     string
		want    bool
		wantErr bool
	}{
		{name: "true", kv: directive.KV{Key: "skip", Value: "true"}, key: "skip", want: true},
		{name: "false", kv: directive.KV{Key: "skip", Value: "false"}, key: "skip", want: false},
		{
			name: "absent is false",
			kv:   directive.KV{Key: "skip", Value: "true"},
			key:  "force",
			want: false,
		},
		{
			name:    "non-boolean is an error",
			kv:      directive.KV{Key: "skip", Value: "yes"},
			key:     "skip",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := newDirective(tt.kv).Bool(tt.key)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestDirectiveInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kv      directive.KV
		key     string
		want    int
		wantErr bool
	}{
		{name: "integer", kv: directive.KV{Key: "behind", Value: "2"}, key: "behind", want: 2},
		{
			name: "negative integer",
			kv:   directive.KV{Key: "behind", Value: "-1"},
			key:  "behind",
			want: -1,
		},
		{
			name: "absent is zero",
			kv:   directive.KV{Key: "behind", Value: "2"},
			key:  "offset",
			want: 0,
		},
		{
			name:    "non-integer is an error",
			kv:      directive.KV{Key: "behind", Value: "two"},
			key:     "behind",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := newDirective(tt.kv).Int(tt.key)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestDirectiveDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kv      directive.KV
		key     string
		want    time.Duration
		wantErr bool
	}{
		{
			name: "hours",
			kv:   directive.KV{Key: "cooldown", Value: "72h"},
			key:  "cooldown",
			want: 72 * time.Hour,
		},
		{
			name: "weeks and days",
			kv:   directive.KV{Key: "cooldown", Value: "2w3d"},
			key:  "cooldown",
			want: 17 * 24 * time.Hour,
		},
		{
			name: "absent is zero",
			kv:   directive.KV{Key: "cooldown", Value: "72h"},
			key:  "other",
			want: 0,
		},
		{
			name:    "unparseable is an error",
			kv:      directive.KV{Key: "cooldown", Value: "soon"},
			key:     "cooldown",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := newDirective(tt.kv).Duration(tt.key)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
