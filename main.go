// Command clover keeps version references in a codebase synchronised with their
// upstream sources of truth.
package main

import (
	"os"

	"github.com/gechr/clover/internal/command"
	"github.com/gechr/clover/internal/logger"
)

func main() {
	logger.Init()
	os.Exit(command.Run())
}
