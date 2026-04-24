package kongfig

// WithProvenance wraps a decoded config value T with its provenance data,
// as returned by [GetWithProvenance].
type WithProvenance[T any] struct {
	Value T
	Prov  *Provenance
}
