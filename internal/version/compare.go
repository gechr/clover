package version

import "github.com/maruel/natural"

// Compare orders a and b, returning -1, 0, or +1. The numeric segments and the
// semver rule that a release outranks any prerelease are honored as usual, but
// prerelease identifiers are compared with natural (digit-aware) ordering rather
// than lexically. That keeps the conventional alpha < beta < rc progression
// while also ranking multi-digit suffixes correctly (beta9 < beta10), which a
// lexical compare reverses.
func Compare(a, b *Version) int {
	if c := compareSegments(a, b); c != 0 {
		return c
	}
	return comparePrerelease(a.Prerelease(), b.Prerelease())
}

// compareSegments compares the numeric components, treating an absent trailing
// segment as zero so 1.2 and 1.2.0 rank equal.
func compareSegments(a, b *Version) int {
	as, bs := a.Segments64(), b.Segments64()
	for i := range max(len(as), len(bs)) {
		switch av, bv := segment(as, i), segment(bs, i); {
		case av < bv:
			return -1
		case av > bv:
			return 1
		}
	}
	return 0
}

// segment returns the ith component, or zero past the end.
func segment(s []int64, i int) int64 {
	if i < len(s) {
		return s[i]
	}
	return 0
}

// comparePrerelease ranks two prerelease strings naturally, with an empty
// prerelease (a final release) outranking any prerelease per semver.
func comparePrerelease(a, b string) int {
	switch {
	case a == "" && b == "":
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	case natural.Less(a, b):
		return -1
	case natural.Less(b, a):
		return 1
	default:
		return 0
	}
}
