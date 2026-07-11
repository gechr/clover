package tag_test

import (
	"testing"

	"github.com/gechr/clover/internal/tag"
	"github.com/stretchr/testify/require"
)

func TestFilter_Empty(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		filter tag.Filter
		want   bool
	}{
		"zero":    {filter: tag.Filter{}, want: true},
		"all set": {filter: tag.Filter{All: []string{"prod"}}, want: false},
		"any set": {filter: tag.Filter{Any: []string{"eu"}}, want: false},
		"not set": {filter: tag.Filter{Not: []string{"legacy"}}, want: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, tt.filter.Empty())
		})
	}
}

func TestFilter_EmptyFromParse(t *testing.T) {
	t.Parallel()

	empty, err := tag.Parse([]string{""})
	require.NoError(t, err)
	require.True(t, empty.Empty(), "an empty --tag value constrains nothing")

	filter, err := tag.Parse([]string{"a"})
	require.NoError(t, err)
	require.False(t, filter.Empty(), "a tagged value constrains the run")
}
