// Package hashicorp resolves HashiCorp tool versions (Terraform, Vault, Consul,
// Nomad, Packer, Boundary, Sentinel, ...) from HashiCorp's public, unauthenticated
// releases API. It lists a product's releases newest-first, tagging each with the
// prerelease flag and creation date the API supplies. By default it tracks the
// open-source releases; enterprise selects enterprise releases (bare semver), and
// build selects one enterprise build flavor by its +metadata suffix (e.g.
// ent.hsm.fips1403), rendering the full version. Checksums are left to a follower
// reading the product's predictable SHA256SUMS file.
package hashicorp
