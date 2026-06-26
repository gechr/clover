// Package github builds GitHub Actions workflow-command annotations - the
// ::error/::warning/::notice lines a workflow parses from a step's output to
// surface a message inline on a pull request.
package github

import (
	"fmt"
	"strings"
)

// Error returns an ::error workflow annotation locating msg at file:line. An
// empty file omits the location; a non-positive line omits just the line. file
// and msg are escaped per the workflow-command grammar. The result carries no
// trailing newline.
func Error(file string, line int, msg string) string {
	return annotate("error", file, line, msg)
}

// Warning returns a ::warning workflow annotation; see [Error].
func Warning(file string, line int, msg string) string {
	return annotate("warning", file, line, msg)
}

// Notice returns a ::notice workflow annotation; see [Error].
func Notice(file string, line int, msg string) string {
	return annotate("notice", file, line, msg)
}

// annotate builds one workflow-command annotation line.
func annotate(level, file string, line int, msg string) string {
	switch {
	case file == "":
		return fmt.Sprintf("::%s::%s", level, escapeData(msg))
	case line > 0:
		return fmt.Sprintf(
			"::%s file=%s,line=%d::%s",
			level,
			escapeProperty(file),
			line,
			escapeData(msg),
		)
	default:
		return fmt.Sprintf("::%s file=%s::%s", level, escapeProperty(file), escapeData(msg))
	}
}

// escapeData escapes a workflow-command message, where %, CR, and LF are special.
func escapeData(s string) string {
	return strings.NewReplacer(
		"%", "%25",
		"\r", "%0D",
		"\n", "%0A",
	).Replace(s)
}

// escapeProperty escapes a workflow-command property value, which additionally
// reserves the , and : that separate properties.
func escapeProperty(s string) string {
	return strings.NewReplacer(
		"%", "%25",
		"\r", "%0D",
		"\n", "%0A",
		":", "%3A",
		",", "%2C",
	).Replace(s)
}
