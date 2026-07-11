package ratelimit

import "time"

// Option configures a [Transport].
type Option func(*Transport)

// WithClock overrides the time source, for tests.
func WithClock(now func() time.Time) Option {
	return func(t *Transport) { t.now = now }
}
