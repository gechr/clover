package follow_test

import (
	"testing"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider/follow"
	"github.com/gechr/clover/internal/registry"
	"github.com/stretchr/testify/require"
)

func TestCandidate(t *testing.T) {
	t.Parallel()

	old := model.Candidate{Version: "1.3.0", Commit: "old111"}
	fresh := model.Candidate{Version: "1.4.0", Commit: "abc123"}

	reg := registry.New()
	reg.Set("tool", registry.Entry{Old: old, New: fresh})

	tests := []struct {
		name string
		when string
		want model.Candidate
	}{
		{name: "empty selects new", when: "", want: fresh},
		{name: "new selects new", when: "new", want: fresh},
		{name: "old selects old", when: "old", want: old},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := follow.Candidate(reg, "tool", tt.when)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCandidateUnknownSelector(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	reg.Set("tool", registry.Entry{New: model.Candidate{Version: "1.4.0"}})

	_, err := follow.Candidate(reg, "tool", "previous")
	require.EqualError(t, err, `follow: unknown selector "previous"`)
}

func TestCandidateMissingProducer(t *testing.T) {
	t.Parallel()

	reg := registry.New()

	_, err := follow.Candidate(reg, "missing", "")
	require.EqualError(t, err, `follow: producer "missing" has not resolved`)
}
