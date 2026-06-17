// Package report renders mode summaries to the user through clog: per-marker
// lines with smart file:line hyperlinks, plus a closing summary. It is the
// CLI-edge presentation layer - the engine returns data, report turns it into
// output. The logger is injected so output is testable and the pure core stays
// terminal-free.
package report
