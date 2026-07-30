package pipeline_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// --infer skips a wholly commented line, since a version inside one is a
// commented-out field or prose rather than a live pin. A SwiftPM manifest is the
// exception: its tools-version declaration is a comment, so the file defers to
// the route table - which claims that one line and nothing else, leaving the
// prose comment beside it and the dependency pin below it alone.
//
// The Xcode project is the shape the guard next door protects: SWIFT_VERSION
// there is the Swift language mode, which accepts 5.0 or 6.0 and nothing between
// them, so the name matching the <TOOL>_VERSION convention must not be enough to
// claim it.
func TestInferReadsPackageSwiftToolsVersion(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "swift",
		candidates: []model.Candidate{candidate(t, "6.3.3")},
	})

	dir := write(t, map[string]string{
		"Package.swift": "// swift-tools-version: 6.0\n" +
			"// pinned against swift-log 1.5.3, see the changelog\n" +
			"import PackageDescription\n" +
			"let package = Package(\n" +
			"    name: \"Demo\",\n" +
			"    dependencies: [\n" +
			"        .package(url: \"https://github.com/apple/swift-log\", from: \"1.5.3\"),\n" +
			"    ]\n" +
			")\n",
		"Demo.xcodeproj/project.pbxproj": "\t\t\t\tSWIFT_VERSION = 6.0;\n",
	})

	files, err := pipeline.Run(
		context.Background(),
		[]string{dir},
		pipeline.WithInfer(true),
		pipeline.WithRequireDirective(false),
	)
	require.NoError(t, err)

	var results []pipeline.Result
	for _, f := range files {
		results = append(results, f.Results...)
	}
	require.Len(t, results, 1, "the declaration is the only line claimed in the tree")

	r := results[0]
	require.Equal(t, filepath.Join(dir, "Package.swift"), r.Marker.File)
	require.Equal(t, 0, r.Marker.Target)
	require.Equal(t, "6.3.3", r.Resolved)
	require.Equal(t, "6.3", r.Written, "a two-part floor stays two-part")
	require.Equal(t, "// swift-tools-version: 6.3", r.NewLine)
}
