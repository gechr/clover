package command

import (
	"context"
	"fmt"

	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/x/set"
)

// predictorTag names the dynamic predictor bound to the --tag flags; the shell
// completion script calls back into clover asking to expand it.
const predictorTag = "tag"

// completionHandler answers a dynamic shell-completion callback. The generated
// completion script re-invokes clover naming the predictor to expand, and the
// handler prints one candidate per line for the shell to offer.
func completionHandler(_, kind string, _ []string) {
	if kind == predictorTag {
		completeTags()
	}
}

// completeTags scans the working tree for directives and prints each unique tag
// label, naturally sorted, so `clover run --tag <TAB>` offers the tags the
// codebase actually uses. Any scan error yields no candidates rather than noise.
func completeTags() {
	files, _, err := pipeline.Scan(context.Background(), []string{"."},
		pipeline.WithConfig(config.NewResolver(nil, "", true)))
	if err != nil {
		return
	}
	tags := set.New[string]()
	for _, file := range files {
		for _, located := range file.Found {
			tags.Add(located.Directive.CSV(constant.DirectiveTags)...)
		}
	}
	for _, tag := range set.SortedNatural(tags) {
		fmt.Println(tag)
	}
}
