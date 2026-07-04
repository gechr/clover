// Command clover keeps version references in a codebase synchronised with their
// upstream sources of truth.
package main

import (
	"os"

	"github.com/gechr/clover/internal/command"
)

func main() {
	os.Exit(command.Run())
}
