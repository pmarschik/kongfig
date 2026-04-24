package kongfig

// SourceFor returns the SourceMeta for the layer that last set path.
// Returns (sm, true) if the path has provenance; (SourceMeta{}, false) if untracked.
func (k *Kongfig) SourceFor(path string) (SourceMeta, bool) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	sm, ok := k.prov.sources[path]
	return sm, ok
}

// Provenance returns a snapshot of the current provenance data.
func (k *Kongfig) Provenance() *Provenance {
	k.mu.RLock()
	defer k.mu.RUnlock()
	snap := NewProvenance()
	for path, sm := range k.prov.sources {
		snap.Set(path, sm)
	}
	return snap
}
