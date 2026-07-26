package pipeline

import (
	"fmt"
	"strings"

	xslices "github.com/gechr/x/slices"
)

// idVerdicts is what the collision pre-pass decided for the markers publishing a
// duplicated id, keyed by marker index. Errs are the hard failures - a duplicate
// the user wrote - and Skips the followers degraded alongside a manufactured one.
// A marker in neither map is unaffected.
type idVerdicts struct {
	Errs  map[int]error
	Skips map[int]string
}

// resolveIDCollisions settles every id published by more than one marker before
// anything resolves, so which producer a follower binds to is never decided by
// scheduling. The executor, the filter map, and the registry each break the tie
// silently - last writer wins, in whichever order the workers ran - and a
// follower cross-wired to the wrong file's producer is exactly the corruption
// ids exist to prevent.
//
// The policy is tiered by provenance, which [Marker.WrittenID] records:
//
//   - A duplicated id the user wrote is a hard error on every written publisher.
//     They created the ambiguity, and only they know which line was meant; the
//     fix is a rename.
//   - An id Clover inferred yields. The marker keeps resolving - the version
//     stays tracked, exactly as it was before pairing existed - but stops
//     publishing, and every same-file follower whose inferred `from` named it is
//     skipped with a reason. The inferred edge dies with the inferred id it was
//     born beside, so a dangling cross-wire is unrepresentable; a *written*
//     `from=` is left alone and binds to the surviving publisher, which is what
//     writing it means. Erroring instead would punish the user for Clover's own
//     inference: two files that each pair GO_VERSION with GO_SHA256 are both
//     correct on their own, and `run --infer` manufactures that shape in every
//     file of a clean tree.
//
// Among inferred publishers alone the first in marker order survives - file
// order, so path order - making the outcome deterministic and idempotent.
//
// Markers is mutated in place (a degraded marker's ID is cleared) and must not
// yet have been copied into result slots.
func resolveIDCollisions(markers []Marker) idVerdicts {
	v := idVerdicts{Errs: make(map[int]error), Skips: make(map[int]string)}

	publishers := make(map[string][]int)
	for i, m := range markers {
		if m.ID != "" {
			publishers[m.ID] = append(publishers[m.ID], i)
		}
	}

	for id, idxs := range publishers {
		if len(idxs) == 1 {
			continue
		}
		written := xslices.Filter(idxs, func(i int) bool { return markers[i].WrittenID })
		if len(written) > 1 {
			for _, i := range written {
				v.Errs[i] = duplicateIDError(markers, id, written)
			}
		}
		survivor := -1
		switch len(written) {
		case 0:
			survivor = idxs[0] // marker order is file order, so path order
		case 1:
			survivor = written[0]
		}
		for _, i := range idxs {
			if i != survivor && !markers[i].WrittenID {
				degradeInferredID(markers, i, id, survivor, &v)
			}
		}
	}
	return v
}

// duplicateIDError reports a written duplicate, naming every publisher so each
// colliding marker's error shows the whole ambiguity rather than its own half.
func duplicateIDError(markers []Marker, id string, written []int) error {
	locations := xslices.Map(written, func(i int) string {
		return fmt.Sprintf("%s:%d", markers[i].File, markers[i].Line+1)
	})
	return fmt.Errorf(
		"duplicate id %q (%s): rename one so followers bind unambiguously",
		bareID(id),
		strings.Join(locations, ", "),
	)
}

// degradeInferredID takes back an id Clover manufactured: marker i stops
// publishing, and every same-file follower whose inferred `from` named the id is
// skipped with a pointer at the survivor and the handwritten way out.
func degradeInferredID(markers []Marker, i int, id string, survivor int, v *idVerdicts) {
	markers[i].ID = ""
	reason := fmt.Sprintf(
		"id %q is published elsewhere - write id=/from= by hand to follow this file's pin",
		bareID(id),
	)
	if survivor >= 0 {
		reason = fmt.Sprintf(
			"id %q is claimed by %s:%d - write id=/from= by hand to follow this file's pin",
			bareID(id),
			markers[survivor].File,
			markers[survivor].Line+1,
		)
	}
	for j, m := range markers {
		if m.IsFollower() && !m.WrittenFrom && m.From == id && m.File == markers[i].File {
			v.Skips[j] = reason
		}
	}
}
