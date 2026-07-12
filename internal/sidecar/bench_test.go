package sidecar_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/sidecar"
)

// benchDoc builds a package-lock.json-shaped document with n packages, each
// carrying a version, resolved URL, and integrity hash - the dominant
// real-world shape for large JSON sidecar targets.
func benchDoc(n int) []string {
	var b strings.Builder
	b.WriteString("{\n  \"name\": \"app\",\n  \"lockfileVersion\": 3,\n  \"packages\": {\n")
	for i := range n {
		fmt.Fprintf(&b, "    \"node_modules/pkg-%04d\": {\n", i)
		fmt.Fprintf(&b, "      \"version\": \"1.%d.0\",\n", i)
		fmt.Fprintf(
			&b,
			"      \"resolved\": \"https://registry.npmjs.org/pkg-%04d/-/pkg-%04d-1.%d.0.tgz\",\n",
			i, i, i,
		)
		fmt.Fprintf(&b, "      \"integrity\": \"sha512-%060d\",\n", i)
		b.WriteString("      \"license\": \"MIT\"\n")
		if i == n-1 {
			b.WriteString("    }\n")
		} else {
			b.WriteString("    },\n")
		}
	}
	b.WriteString("  }\n}\n")
	return strings.Split(b.String(), "\n")
}

// benchDirectives builds count jq-locator directives spread across the doc's
// packages, the way an annotate-generated sidecar tracks a subset of them.
func benchDirectives(packages, count int) []directive.Directive {
	out := make([]directive.Directive, 0, count)
	for i := range count {
		pkg := i * packages / count
		expr := fmt.Sprintf(`.["packages"]["node_modules/pkg-%04d"]["version"]`, pkg)
		out = append(out, directive.Directive{Pairs: []directive.KV{
			{Key: constant.DirectiveProvider, Value: "npm"},
			{Key: constant.DirectiveJQ, Value: expr},
		}})
	}
	return out
}

// BenchmarkLocateSidecar models resolveSidecar: one target file, every entry
// located via its jq locator through a shared Locator.
func BenchmarkLocateSidecar(b *testing.B) {
	for _, tc := range []struct{ packages, entries int }{
		{200, 20},
		{2000, 50},
		{2000, 200},
	} {
		name := fmt.Sprintf("pkgs=%d/entries=%d", tc.packages, tc.entries)
		b.Run(name, func(b *testing.B) {
			lines := benchDoc(tc.packages)
			dirs := benchDirectives(tc.packages, tc.entries)
			b.ReportAllocs()
			for b.Loop() {
				locator := sidecar.NewLocator(lines)
				for _, d := range dirs {
					if _, err := locator.Locate(d); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

// BenchmarkLeaves models annotate's leaf enumeration over the same document.
func BenchmarkLeaves(b *testing.B) {
	for _, packages := range []int{200, 2000} {
		b.Run(fmt.Sprintf("pkgs=%d", packages), func(b *testing.B) {
			source := []byte(strings.Join(benchDoc(packages), "\n"))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := sidecar.Leaves(source); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
