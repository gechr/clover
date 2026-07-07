package comment

import (
	"path/filepath"
	"strings"
)

// Comment delimiter literals, named once because the lookup tables reuse them
// across many formats.
const (
	markerHash  = "#"
	markerSlash = "//"
	markerDash  = "--"
	markerSemi  = ";"

	blockCOpen    = "/*"
	blockCClose   = "*/"
	blockXMLOpen  = "<!--"
	blockXMLClose = "-->"
)

// cBlock is the /* */ pair shared by every C-family syntax.
var cBlock = Block{Open: blockCOpen, Close: blockCClose}

// Reusable comment syntaxes, shared across the lookup tables below.
var (
	hash   = Syntax{Line: []string{markerHash}}
	slash  = Syntax{Line: []string{markerSlash}, Blocks: []Block{cBlock}}
	hcl    = Syntax{Line: []string{markerHash, markerSlash}, Blocks: []Block{cBlock}}
	cstyle = Syntax{Blocks: []Block{cBlock}}
	xml    = Syntax{Blocks: []Block{{Open: blockXMLOpen, Close: blockXMLClose}}}
	dash   = Syntax{Line: []string{markerDash}}
	semi   = Syntax{Line: []string{markerSemi}}

	// generic is the fallback for unrecognised files: the two near-universal
	// line-comment markers. clover is format-agnostic, so an unknown text file can
	// still carry a directive after a # or //.
	generic = Syntax{Line: []string{markerHash, markerSlash}}
)

// byExt maps a lowercased file extension (with leading dot) to its syntax.
var byExt = map[string]Syntax{
	".bash":       hash,
	".cfg":        hash,
	".conf":       hash,
	".dockerfile": hash,
	".env":        hash,
	".fish":       hash,
	".mk":         hash,
	".pl":         hash,
	".py":         hash,
	".r":          hash,
	".rb":         hash,
	".sh":         hash,
	".toml":       hash,
	".yaml":       hash,
	".yml":        hash,
	".zsh":        hash,

	".c":     slash,
	".cc":    slash,
	".cpp":   slash,
	".cs":    slash,
	".go":    slash,
	".h":     slash,
	".hpp":   slash,
	".java":  slash,
	".js":    slash,
	".json5": slash,
	".jsonc": slash,
	".jsx":   slash,
	".kt":    slash,
	".php":   slash,
	".rs":    slash,
	".scala": slash,
	".swift": slash,
	".ts":    slash,
	".tsx":   slash,

	".hcl": hcl,
	".tf":  hcl,

	".css": cstyle,

	".htm":      xml,
	".html":     xml,
	".markdown": xml,
	".md":       xml,
	".svg":      xml,
	".vue":      xml,
	".xml":      xml,

	".lua": dash,
	".sql": dash,

	".ini": semi,
}

// byName maps an exact base name (extensionless configs and dotfiles) to its
// syntax. Checked before byExt.
var byName = map[string]Syntax{
	".bashrc":        hash,
	".dockerignore":  hash,
	".gitattributes": hash,
	".gitignore":     hash,
	".profile":       hash,
	".zshrc":         hash,
	"GNUmakefile":    hash,
	"Makefile":       hash,
	"go.mod":         slash,
}

// For returns the comment syntax for the file at path, matched by base name
// first (dotfiles, Makefile), then a Dockerfile/Containerfile prefix, then file
// extension. An unrecognised file falls back to generic # and // parsing rather
// than being skipped, so a directive in any text file is still found.
func For(path string) Syntax {
	base := filepath.Base(path)
	if s, found := byName[base]; found {
		return s
	}
	if strings.HasPrefix(base, "Dockerfile") || strings.HasPrefix(base, "Containerfile") {
		return hash
	}
	if s, found := byExt[strings.ToLower(filepath.Ext(path))]; found {
		return s
	}
	return generic
}
