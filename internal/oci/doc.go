// Package oci is a small client for the OCI distribution (registry v2) protocol:
// listing a repository's tags, resolving a tag's manifest digest, and answering
// the bearer-token challenge a registry returns on the first unauthenticated
// request. It is the shared heavy lifting behind every clover provider that
// talks to a registry - container images and Helm OCI charts alike speak the
// same wire protocol, so the transport, auth, and pagination live here once.
package oci
