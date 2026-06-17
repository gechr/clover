package comment

import (
	"path/filepath"
	"strings"
)

// Reusable comment syntaxes, shared across the lookup tables below.
var (
	hash  = Syntax{Line: []string{"#"}}
	slash = Syntax{Line: []string{"//"}, Blocks: []Block{{Open: "/*", Close: "*/"}}}
	hcl   = Syntax{Line: []string{"#", "//"}, Blocks: []Block{{Open: "/*", Close: "*/"}}}
	xml   = Syntax{Blocks: []Block{{Open: "<!--", Close: "-->"}}}
	dash  = Syntax{Line: []string{"--"}}
	semi  = Syntax{Line: []string{";"}}

	// generic is the fallback for unrecognised files: the two near-universal
	// line-comment markers. cusp is format-agnostic, so an unknown text file can
	// still carry a directive after a # or //.
	generic = Syntax{Line: []string{"#", "//"}}
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
