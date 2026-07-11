package mode_test

import (
	"testing"

	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/stretchr/testify/require"
)

func outcome(results ...pipeline.Result) mode.Outcome {
	return mode.Outcome{FileResult: pipeline.FileResult{Results: results}}
}

func TestSummary_Disabled(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		summary mode.Summary
		want    int
	}{
		"empty": {
			summary: mode.Summary{},
			want:    0,
		},
		"mixed flags": {
			summary: mode.Summary{Outcomes: []mode.Outcome{
				outcome(
					pipeline.Result{Disabled: true},
					pipeline.Result{Changed: true},
				),
			}},
			want: 1,
		},
		"multiple outcomes summed": {
			summary: mode.Summary{Outcomes: []mode.Outcome{
				outcome(pipeline.Result{Disabled: true}, pipeline.Result{Disabled: true}),
				outcome(pipeline.Result{Disabled: true}, pipeline.Result{Skipped: true}),
			}},
			want: 3,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, tt.summary.Disabled())
		})
	}
}
