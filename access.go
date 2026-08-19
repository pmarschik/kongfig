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
	out := make([]Layer, len(k.pipeline))
	for i := range k.pipeline {
		out[i] = k.pipeline[i].layer
		out[i].Data = k.pipeline[i].layer.Data.Clone()
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

// KeyOrder returns the document key order the loaded layers reported, merged into
// one parent-path → ordered-child-names map. Returns nil when no layer reported an
// order.
//
// Layers are merged in pipeline order and the first layer to mention a key at a
// parent path fixes where it reads; a later layer that re-states the key does not
// move it, and keys only a later layer introduces follow the ones that do. Renderers
// consume this through [RenderDerivedKeyOrderKey], which [Kongfig.Render] binds
// for the merged view — see [WithRenderKeyOrder] to override it, and
// [WithRenderKeySort] to reorder what it produced.
func (k *Kongfig) KeyOrder() map[string][]string {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.keyOrderLocked()
}

// keyOrderLocked merges the pipeline's per-layer key orders. The caller holds
// k.mu.
func (k *Kongfig) keyOrderLocked() map[string][]string {
	merged := make(map[string][]string)
	seen := make(map[string]map[string]bool)
	for i := range k.pipeline {
		for parent, keys := range k.pipeline[i].layer.KeyOrder {
			if seen[parent] == nil {
				seen[parent] = make(map[string]bool, len(keys))
			}
			for _, key := range keys {
				if seen[parent][key] {
					continue
				}
				seen[parent][key] = true
				merged[parent] = append(merged[parent], key)
			}
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}
