package command

import "github.com/gechr/clive"

// module is clover's module path, so clive can build release/commit links.
const module = "github.com/gechr/clover"

// versionCmd prints clover's version, or detailed build information with -d.
type versionCmd struct {
	Detailed bool `help:"Show detailed build information."`
}

// Run prints the version information.
func (c *versionCmd) Run() error {
	if c.Detailed {
		clive.Info{Module: module}.PrintDetailed()
		return nil
	}
	clive.Print()
	return nil
}
