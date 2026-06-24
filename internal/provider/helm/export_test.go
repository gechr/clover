package helm

// ReferenceURL builds a reference and returns its upstream web page, exposing the
// unexported url method for black-box tests.
func ReferenceURL(registry, chart string) (string, error) {
	ref, err := newReference(registry, chart)
	if err != nil {
		return "", err
	}
	return ref.url(), nil
}
