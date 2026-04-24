package kongfig

import "maps"

// Strict returns a [GetOption] that makes [Get] fail if any struct field has no matching key.
func Strict() GetOption { return bindGet(getStrictKey, true) }

// At returns a [GetOption] that decodes the sub-tree at the given dot-delimited path.
func At(path string) GetOption { return bindGet(getPathKey, path) }

// WithPathMeta registers per-path typed metadata on the Kongfig instance.
// The entries are injected into the render context by [Kongfig.RenderWith]
// and can be read at render time via [PathMetaKey.Get] and [PathMetaKey.GetAll].
//
// Use this to attach renderer-specific hints derived from struct annotations:
//
//	var SplitSepKey = kongfig.NewPathMetaKey[string]()
//	kf := kongfig.New(kongfig.WithPathMeta(SplitSepKey, map[string]string{"tags": ","}))
func WithPathMeta[T any](key PathMetaKey[T], entries map[string]T) Option {
	return func(k *Kongfig) {
		k.mu.Lock()
		defer k.mu.Unlock()
		if existing, ok := k.cfg.pathMeta[key].(map[string]T); ok {
			maps.Copy(existing, entries)
			k.cfg.pathMeta[key] = existing
		} else {
			k.cfg.pathMeta[key] = maps.Clone(entries)
		}
	}
}
