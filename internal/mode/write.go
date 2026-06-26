package mode

import (
	"fmt"
	"os"
	"strings"

	xos "github.com/gechr/x/os"
)

// writeFile atomically replaces path's contents with lines joined by newlines,
// preserving the file's existing mode exactly. clover never changes a file's
// permissions - managing those is the user's responsibility - so a file's mode
// is read and re-applied verbatim, and a file whose mode cannot be read is left
// untouched (an error) rather than written with an invented one.
func writeFile(path string, lines []string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	return xos.AtomicWrite(path, []byte(strings.Join(lines, "\n")), info.Mode().Perm())
}
