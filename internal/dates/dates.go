package dates

import (
	"encoding/json"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const endOfDayOffset = 24*time.Hour - time.Second

// ReleaseTime is provider release metadata decoded for cooldown. Full
// timestamps are kept exact; date-only values are interpreted as the last second
// of that UTC day so a release is not treated as old too early.
type ReleaseTime struct {
	time.Time
}

// ParseReleaseTime parses a provider release time. It accepts full RFC3339
// timestamps and YYYY-MM-DD dates, using 23:59:59 UTC for date-only metadata.
func ParseReleaseTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		return time.Time{}, err
	}
	return t.Add(endOfDayOffset), nil
}

func (t *ReleaseTime) UnmarshalJSON(data []byte) error {
	if strings.TrimSpace(string(data)) == "null" {
		t.Time = time.Time{}
		return nil
	}
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	parsed, err := ParseReleaseTime(raw)
	if err != nil {
		return err
	}
	t.Time = parsed
	return nil
}

func (t *ReleaseTime) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag == "!!null" {
		t.Time = time.Time{}
		return nil
	}
	parsed, err := ParseReleaseTime(node.Value)
	if err != nil {
		return err
	}
	t.Time = parsed
	return nil
}
