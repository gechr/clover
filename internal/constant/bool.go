package constant

// Boolean directive values. A bare boolean key (one written without =) means
// True; an explicit value must be exactly one of these - strict, so a typo like
// skip=yes is rejected rather than silently treated as false.
const (
	True  = "true"
	False = "false"
)
