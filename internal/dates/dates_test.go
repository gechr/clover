package dates_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gechr/clover/internal/dates"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestParseReleaseTime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want time.Time
	}{
		{
			name: "timestamp",
			raw:  "2026-01-02T03:04:05.123Z",
			want: time.Date(2026, 1, 2, 3, 4, 5, 123_000_000, time.UTC),
		},
		{
			name: "date only",
			raw:  "2026-01-02",
			want: time.Date(2026, 1, 2, 23, 59, 59, 0, time.UTC),
		},
		{
			name: "empty",
			raw:  "",
			want: time.Time{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := dates.ParseReleaseTime(tt.raw)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestReleaseTimeUnmarshalJSON(t *testing.T) {
	t.Parallel()

	var decoded struct {
		Published dates.ReleaseTime `json:"published"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{"published":"2026-01-02"}`), &decoded))
	require.Equal(t,
		time.Date(2026, 1, 2, 23, 59, 59, 0, time.UTC),
		decoded.Published.Time,
	)

	require.NoError(t, json.Unmarshal([]byte(`{"published":null}`), &decoded))
	require.True(t, decoded.Published.IsZero())
}

func TestReleaseTimeUnmarshalYAML(t *testing.T) {
	t.Parallel()

	var decoded struct {
		Created dates.ReleaseTime `yaml:"created"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(`created: "2026-01-02"`), &decoded))
	require.Equal(t,
		time.Date(2026, 1, 2, 23, 59, 59, 0, time.UTC),
		decoded.Created.Time,
	)

	var nullDecoded struct {
		Created dates.ReleaseTime `yaml:"created"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(`created: null`), &nullDecoded))
	require.True(t, nullDecoded.Created.IsZero())
}
