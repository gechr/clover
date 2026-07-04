package provider

import "context"

// floorKey is the unexported context key under which the version floor is
// carried.
type floorKey struct{}

// WithVersionFloor returns a context carrying the marker's current version. The
// pipeline sets it only when selection cannot pick anything below that version
// (downgrades off), so a provider whose listing is version-ordered may stop
// paging once it passes the floor - every later page holds only versions that
// could never be selected.
func WithVersionFloor(ctx context.Context, floor string) context.Context {
	return context.WithValue(ctx, floorKey{}, floor)
}

// VersionFloor returns the version floor for this lookup, "" when selection may
// reach below the current version and pagination must run to exhaustion.
func VersionFloor(ctx context.Context) string {
	floor, _ := ctx.Value(floorKey{}).(string)
	return floor
}
