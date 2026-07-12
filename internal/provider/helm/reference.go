package helm

import (
	"fmt"
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/oci"
	xstrings "github.com/gechr/x/strings"
)

// reference is helm's validated descriptor. A chart is addressed either by a
// classic chart repository (an https:// URL serving index.yaml) or by an OCI
// registry (an oci:// base under which the chart's tags are its versions).
type reference struct {
	chart string
	isOCI bool

	// Classic repository fields.
	baseURL  string // repository root, e.g. https://charts.bitnami.com/bitnami
	indexURL string // baseURL + /index.yaml

	// OCI registry fields.
	repo oci.Repo
}

// newReference validates a registry value and chart name. The registry scheme
// selects the backend: https:// (or http://) is a classic repository read from
// index.yaml; oci:// lists the chart's tags from the registry. The chart is a
// bare name - a path belongs in the registry, so a slash is rejected.
func newReference(registry, chart string) (reference, error) {
	chart = strings.TrimSpace(chart)
	switch {
	case chart == "":
		return reference{}, fmt.Errorf("helm: %q is required", keyChart)
	case strings.ContainsAny(chart, " \t"):
		return reference{}, fmt.Errorf(
			"helm: %q must not contain whitespace, got %q",
			keyChart,
			chart,
		)
	case strings.Contains(chart, "/"):
		return reference{}, fmt.Errorf(
			"helm: put the repository path in %q, not %q (got %q)",
			keyRegistry, keyChart, chart,
		)
	}

	registry = strings.TrimSpace(registry)
	scheme, rest, ok := strings.Cut(registry, "://")
	if !ok {
		return reference{}, fmt.Errorf(
			"helm: %q must start with https://, http:// or oci://, got %q", keyRegistry, registry,
		)
	}

	switch strings.ToLower(scheme) {
	case "https", "http":
		base := strings.TrimSuffix(registry, "/")
		return reference{
			chart:    chart,
			baseURL:  base,
			indexURL: base + "/index.yaml",
		}, nil
	case "oci":
		host, path, _ := strings.Cut(strings.Trim(rest, "/"), "/")
		if host == "" {
			return reference{}, fmt.Errorf(
				"helm: %s %q has no registry host",
				keyRegistry,
				registry,
			)
		}
		repository := chart
		if path != "" {
			repository = path + "/" + chart
		}
		return reference{
			chart: chart,
			isOCI: true,
			repo:  oci.Repo{Host: host, Repository: repository},
		}, nil
	default:
		return reference{}, fmt.Errorf(
			"helm: %s %q has an unsupported scheme %q (use https://, http:// or oci://)",
			keyRegistry, registry, scheme,
		)
	}
}

// String returns a human-readable label for the reference.
func (r reference) String() string {
	if r.isOCI {
		return "oci://" + r.repo.Host + "/" + r.repo.Repository
	}
	return xstrings.TrimPrefixes(
		r.baseURL,
		constant.SchemeHTTPS,
		constant.SchemeHTTP,
	) + "/" + r.chart
}

// url is the reference's upstream web page: an OCI registry's repository over
// https, or a classic repository's chart under its base URL (scheme kept).
func (r reference) url() string {
	if r.isOCI {
		return constant.SchemeHTTPS + r.repo.Host + "/" + r.repo.Repository
	}
	return r.baseURL + "/" + r.chart
}
