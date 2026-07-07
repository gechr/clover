package command

import (
	"context"
	"fmt"

	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/x/set"
)

// Names of the dynamic predictors bound to flags; the shell completion script
// calls back into clover naming the one to expand.
const (
	predictorProvider = "provider" // --enable / --disable
	predictorTag      = "tag"      // --tag
)

// completionHandler answers a dynamic shell-completion callback. The generated
// completion script re-invokes clover naming the predictor to expand, and the
// handler prints one candidate per line for the shell to offer.
func completionHandler(_, kind string, _ []string) {
	switch kind {
	case predictorProvider:
		completeProviders()
	case predictorTag:
		completeTags()
	}
}

// completeProviders prints every provider --enable and --disable accept, so
// `clover run --enable <TAB>` offers the selectable providers.
func completeProviders() {
	for _, name := range provider.Selectable() {
		fmt.Println(name)
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
