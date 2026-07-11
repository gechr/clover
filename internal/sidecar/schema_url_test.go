package sidecar_test

import (
	"encoding/json"
	"testing"

	"github.com/gechr/clover/internal/sidecar"
	"github.com/stretchr/testify/require"
)

// TestSchemaURL confirms the published URL is read straight from the embedded
// schema's own $id, so the document stays the single source of truth for the URL
// a docs modeline points at.
func TestSchemaURL(t *testing.T) {
	t.Parallel()

	var doc struct {
		ID string `json:"$id"`
	}
	require.NoError(t, json.Unmarshal(sidecar.Schema(), &doc))
	require.NotEmpty(t, doc.ID)
	require.Equal(t, doc.ID, sidecar.SchemaURL())
}
