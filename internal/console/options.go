package console

// Option configures a [Reporter].
type Option func(*Reporter)

// WithInfer marks the run as inference-driven: a tree carrying no clover:
// comments is the expected input there, so Discovered reports it as
// information rather than a warning.
func WithInfer(on bool) Option {
	return func(r *Reporter) { r.infer = on }
}
