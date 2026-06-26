package constant

// Follow value selectors: whether a follow marker reads the producer's value
// from before the run (Old) or after it resolved (New, the default).
const (
	FollowNew = "new"
	FollowOld = "old"
)
