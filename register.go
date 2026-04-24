package kongfig

// MergeFunc is a custom merge strategy for a specific config path.
// It receives the current destination value and the incoming source value,
// and must return the merged result. Return a non-nil error to fall back
// to last-writer-wins semantics. It must not reference the Kongfig instance.
type MergeFunc func(dst, src any) (any, error)

// OnLoad registers a hook called after each successful Load.
// If any hook returns a LoadResult with a non-nil Err, Load propagates that error.
// All hooks are called regardless; errors are joined via errors.Join.
func (k *Kongfig) OnLoad(fn LoadFunc) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.hooks.onLoad = append(k.hooks.onLoad, fn)
}

// SetMergeFunc registers a custom merge strategy for the given dot-delimited path.
func (k *Kongfig) SetMergeFunc(path string, fn MergeFunc) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.cfg.mergeFuncs[path] = fn
}
