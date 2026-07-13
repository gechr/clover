package swift

import (
	"cmp"
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

const (
	// tagPrefix and tagSuffix wrap a bare version into its release tag:
	// swift-6.3.3-RELEASE.
	tagPrefix = "swift-"
	tagSuffix = "-RELEASE"
	// releaseBase is the web page a release tag resolves to - swift.org publishes
	// no per-version page of its own.
	releaseBase = constant.SchemeHTTPS + "github.com/swiftlang/swift/releases/tag/"
)

// URL builds the web page for a resolved candidate: the release tag's page on
// the upstream repository. The tag wears both a prefix and a suffix, which
// only a discovered candidate's Ref carries - the synthesized current-version
// candidate arrives bare, since the pipeline reconstructs only a v-style
// prefix - so the ref is normalized to its bare core and the full tag is
// rebuilt here. Empty when the version is unknown.
func (p *Provider) URL(r provider.Resource, c model.Candidate) string {
	ref := cmp.Or(c.Ref, c.Version)
	if _, ok := r.(resource); !ok || ref == "" {
		return ""
	}
	core := strings.TrimSuffix(strings.TrimPrefix(ref, tagPrefix), tagSuffix)
	return releaseBase + tagPrefix + core + tagSuffix
}
