package sidecar

import (
	_ "embed"
	"encoding/json"
)

// schemaJSON is the sidecar JSON schema, published for editors (a
// `# yaml-language-server: $schema=<url>` modeline in a sidecar lights up
// completion and validation as it is typed). Clover itself validates sidecars
// through the directive grammar, not this schema; a drift test keeps the schema's
// property list in lockstep with that grammar.
//
//go:embed schema.json
var schemaJSON []byte

// Schema returns the embedded sidecar JSON schema document.
func Schema() []byte { return schemaJSON }

// SchemaURL returns the schema's published location, read from its own $id so the
// document is the single source of truth for the URL a docs modeline points at.
func SchemaURL() string {
	var doc struct {
		ID string `json:"$id"`
	}
	_ = json.Unmarshal(schemaJSON, &doc)
	return doc.ID
}
