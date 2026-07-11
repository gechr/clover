package dates_test

import (
	"encoding/json"
	"testing"

	"github.com/gechr/clover/internal/dates"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestParseReleaseTimeInvalid(t *testing.T) {
	t.Parallel()

	got, err := dates.ParseReleaseTime("not-a-date")
	require.Error(t, err)
	require.True(t, got.IsZero())
}

func TestReleaseTimeUnmarshalJSONInvalid(t *testing.T) {
	t.Parallel()

	var decoded struct {
		Published dates.ReleaseTime `json:"published"`
	}

	require.Error(t,
		json.Unmarshal([]byte(`{"published":123}`), &decoded),
		"a non-string value cannot unmarshal into ReleaseTime")
	require.Error(t,
		json.Unmarshal([]byte(`{"published":"bad"}`), &decoded),
		"an unparseable date string is an error")
}

func TestReleaseTimeUnmarshalYAMLInvalid(t *testing.T) {
	t.Parallel()

	var decoded struct {
		Created dates.ReleaseTime `yaml:"created"`
	}

	require.Error(t,
		yaml.Unmarshal([]byte(`created: "bad"`), &decoded),
		"an unparseable date string is an error")
}

// TestReleaseTimeUnmarshalYAMLNull drives the null node directly: yaml.Unmarshal
// skips the unmarshaler for a null value, so the explicit-null branch is reached
// only by invoking the method with a !!null node.
func TestReleaseTimeUnmarshalYAMLNull(t *testing.T) {
	t.Parallel()

	var rt dates.ReleaseTime
	node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	require.NoError(t, rt.UnmarshalYAML(node))
	require.True(t, rt.IsZero())
}
