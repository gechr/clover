package docker

import (
	"fmt"
	"strings"
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
type reference struct {
	registry   string
	repository string
	dockerHub  bool
}

// newReference validates and normalises a registry host and repository path. An
// empty or Docker Hub registry routes to the Hub API; a bare single-segment
// repository on Docker Hub gains the implicit library/ namespace
// (nginx -> library/nginx), matching docker's own resolution.
func newReference(registry, repository string) (reference, error) {
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

	if dockerHubAliases[registry] {
		if !strings.Contains(repository, "/") {
			repository = hubDefaultNamespace + repository
		}
		return reference{registry: hubAPIHost, repository: repository, dockerHub: true}, nil
	}
	return reference{registry: registry, repository: repository}, nil
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
