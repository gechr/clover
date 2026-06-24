package docker

// AuthHint exposes the auth-hint text so error assertions can match it exactly.
const AuthHint = authHint

// ReferenceURL builds a reference and returns its upstream web page, exposing the
// unexported url method for black-box tests.
func ReferenceURL(registry, repository string) (string, error) {
	ref, err := newReference(registry, repository, "")
	if err != nil {
		return "", err
	}
	return ref.url(), nil
}
