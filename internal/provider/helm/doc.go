// Package helm resolves Helm chart versions from both classic HTTP chart
// repositories (an index.yaml served under the repo URL) and OCI registries
// (oci://, where a chart's versions are the repository's tags). The OCI path
// reuses the shared internal/oci client; the classic path parses the index for
// versions, release dates, and the chart-tarball digest.
package helm
