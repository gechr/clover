// Package terraform resolves Terraform provider versions from a registry
// implementing the provider registry protocol. One implementation serves two
// registered names - provider=terraform (registry.terraform.io) and
// provider=opentofu (registry.opentofu.org) - which differ only in name,
// default host, and where a resolved version's web page lives. The host key
// points either at a private registry.
package terraform
