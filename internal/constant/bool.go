package constant

// Boolean directive values. A boolean key's value must be exactly one of these
// - strict, so a typo like skip=yes is rejected rather than silently treated as
// false.
const (
	BoolFalse = "false"
	BoolTrue  = "true"
)
