package command

import "github.com/gechr/clive"

// module is clover's module path, so clive can build release/commit links.
const module = "github.com/gechr/clover"

// cmdVersion prints clover's version, or detailed build information with -d.
type cmdVersion struct {
	Detailed bool `help:"Show detailed build information" clib:"terse='Detailed build info',group='Options/Output'"`
}

// Run prints the version information.
func (c *cmdVersion) Run() error {
	if c.Detailed {
		clive.Info{Module: module}.PrintDetailed()
		return nil
	}
	clive.Print()
	return nil
}
