package scan

import (
	"fmt"
	"strconv"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/pattern"
)

// Target resolves the line index loc governs within lines. A sidecar entry's
// Line is already the resolved target. An inline directive governs the line
// offset= lines below its comment (default 1, the next line); target= instead
// anchors it to the first line matching the given glob or /regex/, searching
// from the offset downward - robust against content moving between the comment
// and the value. Both the pipeline's bind and the sidecar double-governance
// guard resolve through here, so the two layers cannot disagree.
func (loc Located) Target(lines []string) (int, error) {
	if loc.Sidecar {
		return loc.Line, nil
	}
	offset, err := loc.offset()
	if err != nil {
		return 0, err
	}
	start := loc.Line + offset

	target, ok := loc.Directive.Get(constant.DirectiveTarget)
	if !ok {
		return start, nil
	}
	if target == "" {
		return 0, fmt.Errorf("%q pattern is empty", constant.DirectiveTarget)
	}
	pat, err := pattern.Compile(target)
	if err != nil {
		return 0, fmt.Errorf("%q: %w", constant.DirectiveTarget, err)
	}
	re := pat.Regexp()
	for i := start; i < len(lines); i++ {
		if re.MatchString(lines[i]) {
			return i, nil
		}
	}
	return 0, fmt.Errorf("%q matched no line below the comment", constant.DirectiveTarget)
}

// offset returns the directive's offset= value, defaulting to 1 (the next
// line) when the key is absent.
func (loc Located) offset() (int, error) {
	raw, ok := loc.Directive.Get(constant.DirectiveOffset)
	if !ok {
		return 1, nil
	}
	offset, err := strconv.Atoi(raw)
	if err != nil || offset < 1 {
		return 0, fmt.Errorf("%q must be a positive integer", constant.DirectiveOffset)
	}
	return offset, nil
}
