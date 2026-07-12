package sidecar_test

import (
	"encoding/json"
	"testing"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/all"
	"github.com/gechr/clover/internal/sidecar"
	xmaps "github.com/gechr/x/maps"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestMain(m *testing.M) {
	provider.RegisterAll(all.New("")...)
	m.Run()
}

// TestSchemaCoversEveryDirectiveKey is the drift guard: the sidecar schema's
// property list must equal the directive vocabulary a sidecar entry may carry -
// the common keys, every registered provider's keys, and the jq locator. A new or
// renamed key fails this test until the schema is updated, so the published schema
// can never silently fall behind the grammar.
func TestSchemaCoversEveryDirectiveKey(t *testing.T) {
	want := map[string]bool{constant.DirectiveJQ: true}
	for _, key := range directive.CommonKeys() {
		want[key] = true
	}
	// offset and target anchor an inline comment to a line below it; a sidecar
	// entry is located by its own jq/find, so the schema must not offer them.
	delete(want, constant.DirectiveOffset)
	delete(want, constant.DirectiveTarget)
	for _, name := range provider.Names() {
		prov, ok := provider.Get(name)
		require.True(t, ok)
		for _, key := range prov.Keys() {
			want[key.Name] = true
		}
	}

	got := schemaProperties(t)
	require.ElementsMatch(
		t,
		xmaps.KeysNatural(want),
		got,
		"sidecar schema properties must match the directive vocabulary (common + provider keys + jq)",
	)
}

// TestSchemaValidatesWorkedExample confirms the documented Biome sidecar - the
// worked example in docs/sidecar.md - validates against the schema, and that an
// unknown key and a locator-less entry are both rejected, so the schema actually
// enforces what the docs promise.
func TestSchemaValidatesWorkedExample(t *testing.T) {
	schema := compileSchema(t)

	valid := `
- provider: github
  repository: biomejs/biome
  tag-prefix: "@biomejs/biome@"
  constraint: minor
  jq: '.["$schema"]'
  find: schemas/<version>/schema.json
`
	require.NoError(t, schema.Validate(yamlToAny(t, valid)))

	unknownKey := "- provider: github\n  repositroy: a/b\n  jq: .x\n"
	require.Error(t, schema.Validate(yamlToAny(t, unknownKey)), "an unknown key is rejected")

	noLocator := "- provider: github\n  repository: a/b\n  constraint: minor\n"
	require.Error(
		t,
		schema.Validate(yamlToAny(t, noLocator)),
		"an entry without a jq/find locator is rejected",
	)
}

// schemaProperties reads the property names the sidecar schema declares on an
// entry.
func schemaProperties(t *testing.T) []string {
	t.Helper()
	var doc struct {
		Items struct {
			Properties map[string]any `json:"properties"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(sidecar.Schema(), &doc))
	return xmaps.KeysNatural(doc.Items.Properties)
}

// compileSchema compiles the embedded sidecar schema for validation.
func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	var doc any
	require.NoError(t, json.Unmarshal(sidecar.Schema(), &doc))
	compiler := jsonschema.NewCompiler()
	const id = "sidecar.schema.json"
	require.NoError(t, compiler.AddResource(id, doc))
	compiled, err := compiler.Compile(id)
	require.NoError(t, err)
	return compiled
}

// yamlToAny decodes a YAML document into the any tree the JSON-schema validator
// consumes, mirroring how config loading validates YAML against a JSON schema.
func yamlToAny(t *testing.T, doc string) any {
	t.Helper()
	var v any
	require.NoError(t, yaml.Unmarshal([]byte(doc), &v))
	return v
}
