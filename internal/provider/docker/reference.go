package docker

import (
	"fmt"
	"strings"

	"github.com/gechr/clover/internal/oci"
)

// Registry hosts that all mean Docker Hub. Docker Hub is addressed by several
// aliases (and an empty registry= defaults to it); the canonical registry host
// is registry-1.docker.io, but tag discovery uses the richer hub.docker.com API
// rather than the registry, so the alias is recorded as dockerHub instead.
const (
	hubAPIHost          = "hub.docker.com"       // Docker Hub's web API (newest-first tags, with dates)
	hubRegistryHost     = "registry-1.docker.io" // Docker Hub's OCI registry (manifests, digests)
	hubAuthHost         = "index.docker.io"      // the host docker login stores Hub credentials under
	hubDefaultNamespace = "library/"             // implicit namespace for a bare official image
)

// dockerHubAliases are the registry= values that resolve to Docker Hub.
var dockerHubAliases = map[string]bool{
	"":                        true,
	"docker.io":               true,
	"index.docker.io":         true,
	"registry-1.docker.io":    true,
	"registry.hub.docker.com": true,
	hubAPIHost:                true,
}

// reference is docker's validated descriptor: the registry host and the
// repository path within it, plus whether it points at Docker Hub - which has
// its own, richer tags API and an implicit library/ namespace for bare names.
// platform, when set, pins a specific os/arch manifest digest instead of the
// multi-arch index digest.
type reference struct {
	registry   string
	repository string
	platform   string
	dockerHub  bool
}

// splitHost separates an inline registry host from a repository path when the
// first segment looks like a host. The canonical Docker rule (matching
// `docker pull`): the first segment is a host if it contains a "." or a ":"
// (port), or equals "localhost". So ghcr.io/owner/img splits to host ghcr.io +
// repo owner/img, but library/redis and team/app (no dot) stay whole repository
// paths. A single-segment value never splits.
func splitHost(repository string) (host, repo string) {
	first, rest, ok := strings.Cut(repository, "/")
	if ok && (strings.ContainsAny(first, ".:") || first == "localhost") {
		return first, rest
	}
	return "", repository
}

// newReference validates and normalises a registry host and repository path. An
// empty or Docker Hub registry routes to the Hub API; a bare single-segment
// repository on Docker Hub gains the implicit library/ namespace
// (nginx -> library/nginx), matching docker's own resolution. With no explicit
// registry, an inline host on the repository (ghcr.io/owner/img) is split out, so
// a `docker pull`-shaped reference works without a separate registry=.
func newReference(registry, repository, platform string) (reference, error) {
	registry = strings.TrimSuffix(strings.TrimPrefix(registry, "https://"), "/")
	repository = strings.Trim(repository, "/")
	switch {
	case repository == "":
		return reference{}, fmt.Errorf("docker: %s is required", keyRepository)
	case strings.ContainsAny(repository, " \t"):
		return reference{}, fmt.Errorf(
			"docker: %s %q must not contain whitespace",
			keyRepository,
			repository,
		)
	case strings.Contains(repository, "://"):
		return reference{}, fmt.Errorf(
			"docker: put the registry host in %s, not %s (got %q)",
			keyRegistry, keyRepository, repository,
		)
	}
	if err := validatePlatform(platform); err != nil {
		return reference{}, err
	}

	// Explicit registry= always wins; only an unset registry parses an inline host.
	if registry == "" {
		if host, repo := splitHost(repository); host != "" {
			registry, repository = host, repo
		}
	}

	if dockerHubAliases[registry] {
		if !strings.Contains(repository, "/") {
			repository = hubDefaultNamespace + repository
		}
		return reference{registry: hubAPIHost, repository: repository, platform: platform, dockerHub: true}, nil
	}
	return reference{registry: registry, repository: repository, platform: platform}, nil
}

// validatePlatform rejects a platform that is not os/arch (exactly one slash,
// both halves non-empty, no whitespace). An empty platform is valid and means
// "pin the multi-arch index digest", the default.
func validatePlatform(platform string) error {
	if platform == "" {
		return nil
	}
	os, arch, ok := strings.Cut(platform, "/")
	if !ok || os == "" || arch == "" || strings.ContainsAny(platform, " \t") {
		return fmt.Errorf("docker: %s %q must be os/arch", keyPlatform, platform)
	}
	return nil
}

// String returns a human-readable label for the reference.
func (r reference) String() string {
	if r.dockerHub {
		return "docker.io/" + r.repository
	}
	return r.registry + "/" + r.repository
}

// registryV2Host is the OCI registry host that serves manifests for the
// reference. Docker Hub discovery uses its web API, but digests come from the
// registry, so Hub maps to registry-1.docker.io.
func (r reference) registryV2Host() string {
	if r.dockerHub {
		return hubRegistryHost
	}
	return r.registry
}

// authHost is the host credentials are keyed under. Docker login stores Hub
// credentials under index.docker.io, not the web-API or registry host.
func (r reference) authHost() string {
	if r.dockerHub {
		return hubAuthHost
	}
	return r.registry
}

// ociRepo is the repository for tag discovery on a (non-Hub) OCI registry, where
// the registry host both serves /v2 and keys credentials.
func (r reference) ociRepo() oci.Repo {
	return oci.Repo{Host: r.registry, Repository: r.repository}
}

// manifestRepo is the repository for digest resolution: manifests come from the
// registry v2 host, but credentials are keyed under the (possibly distinct) auth
// host - the two diverge for Docker Hub.
func (r reference) manifestRepo() oci.Repo {
	return oci.Repo{
		Host:       r.registryV2Host(),
		AuthHost:   r.authHost(),
		Repository: r.repository,
		Platform:   r.platform,
	}
}
