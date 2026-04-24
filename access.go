package kongfig

import "strings"

// All returns a deep copy of the merged configuration map.
func (k *Kongfig) All() ConfigData {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.data.Clone()
}

// Flat returns a flat ConfigData mapping dot-delimited paths to their leaf values.
// Values retain their original types (int, bool, string, etc.); no stringification.
// Computed lazily on each call; not cached.
func (k *Kongfig) Flat() ConfigData {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.data.FlatValues()
}

// Layers returns a snapshot of the loaded layers.
// Each Layer.Data map is a deep copy; mutating it does not affect Kongfig state.
func (k *Kongfig) Layers() []Layer {
	k.mu.RLock()
	defer k.mu.RUnlock()
	out := make([]Layer, len(k.layers))
	for i, l := range k.layers {
		out[i] = l
		out[i].Data = l.Data.Clone()
	}
	return out
}

// Exists reports whether the given dot-delimited path exists in the merged config.
func (k *Kongfig) Exists(path string) bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	_, ok := k.data.existsAt(strings.Split(path, "."))
	return ok
}

// Cut returns a new Kongfig containing only the sub-tree at the given dot-delimited path.
// Returns an empty Kongfig if the path does not exist.
func (k *Kongfig) Cut(path string) *Kongfig {
	k.mu.RLock()
	defer k.mu.RUnlock()

	child := New()
	sub := k.data.subTreeAt(strings.Split(path, "."))
	if sub != nil {
		child.data = sub.Clone()
	}
	// Carry provenance for the sub-tree.
	prefix := path + "."
	for _, p := range k.prov.Paths() {
		if after, ok := strings.CutPrefix(p, prefix); ok {
			child.prov.Set(after, k.prov.sources[p])
		} else if p == path {
			child.prov.Set("", k.prov.sources[p])
		}
	}
	return child
}
