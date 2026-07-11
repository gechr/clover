package jq_test

import (
	"testing"

	"github.com/gechr/clover/internal/jq"
	"github.com/stretchr/testify/require"
)

// TestCompileValid confirms a well-formed path program compiles and runs over a
// decoded JSON value, so a returned code is genuinely executable rather than just
// non-nil.
func TestCompileValid(t *testing.T) {
	t.Parallel()

	code, err := jq.Compile(".foo")
	require.NoError(t, err)
	require.NotNil(t, code)

	iter := code.Run(map[string]any{"foo": "bar"})
	value, ok := iter.Next()
	require.True(t, ok)
	require.Equal(t, "bar", value)
}

// TestCompileParseError confirms a program that does not parse returns the gojq
// parse error unwrapped, with no code.
func TestCompileParseError(t *testing.T) {
	t.Parallel()

	code, err := jq.Compile(".[")
	require.Error(t, err)
	require.Nil(t, code)
}

// TestCompileCompileError confirms a program that parses but references an
// undefined function fails at compile time, again returning the gojq error with
// no code.
func TestCompileCompileError(t *testing.T) {
	t.Parallel()

	code, err := jq.Compile(".foo | nosuchfunc")
	require.Error(t, err)
	require.Nil(t, code)
}
