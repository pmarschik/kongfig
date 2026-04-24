package kongfig

import (
	"maps"

	"github.com/pmarschik/kongfig/schema"
)

// mergeCodecPaths merges paths into k.cfg.pathMeta[codecPathsKey], preserving
// any existing entries. Safe to call from Option functions (which run without
// a held lock) and from RegisterCodec (which acquires the lock before calling this).
func mergeCodecPaths(k *Kongfig, paths map[string]anyCodec) {
	if len(paths) == 0 {
		return
	}
	var existing map[string]anyCodec
	if v, ok := k.cfg.pathMeta[codecPathsKey].(map[string]anyCodec); ok {
		existing = v
	}
	updated := make(map[string]anyCodec, len(existing)+len(paths))
	maps.Copy(updated, existing)
	maps.Copy(updated, paths)
	k.cfg.pathMeta[codecPathsKey] = updated
}

// applyBidirectionalCodecs applies path codecs that have an Encode function at
// load time. Decode-only codecs (encode == nil) are intentionally excluded so
// that raw values are preserved in the store for renderers.
func applyBidirectionalCodecs(k *Kongfig, data ConfigData) (ConfigData, error) {
	k.mu.RLock()
	codecs := filterPathCodecs(k, func(ac anyCodec) bool { return ac.encode != nil })
	k.mu.RUnlock()
	if len(codecs) == 0 {
		return data, nil
	}
	return data.applyCodecs(codecs, "")
}

// applyDecodeOnlyCodecs applies path codecs that have no Encode function at
// Get time. The raw value in the store is left unchanged by load; decoding
// happens here so consumers see typed values without affecting rendering.
func applyDecodeOnlyCodecs(k *Kongfig, data ConfigData) (ConfigData, error) {
	k.mu.RLock()
	codecs := filterPathCodecs(k, func(ac anyCodec) bool { return ac.encode == nil && ac.decode != nil })
	k.mu.RUnlock()
	if len(codecs) == 0 {
		return data, nil
	}
	return data.applyCodecs(codecs, "")
}

// filterPathCodecs returns the subset of registered path codecs matching include.
// Caller must hold k.mu.RLock.
func filterPathCodecs(k *Kongfig, include func(anyCodec) bool) map[string]anyCodec {
	if k.cfg.pathMeta == nil {
		return nil
	}
	pc, ok := k.cfg.pathMeta[codecPathsKey].(map[string]anyCodec)
	if !ok {
		return nil
	}
	var out map[string]anyCodec
	for path, ac := range pc {
		if include(ac) {
			if out == nil {
				out = make(map[string]anyCodec)
			}
			out[path] = ac
		}
	}
	return out
}

// withCodecPathResolution returns an Option that resolves schema.CodecPathEntries against
// the codec registry and merges the result into k.cfg.pathMeta[codecPathsKey].
// It must be appended AFTER user options so WithCodec/WithCodecRegistry registrations are visible.
//
// Resolution order for each entry:
//  1. If CodecName is set, look up by name. If not found, fall back to type-based lookup.
//  2. If CodecName is empty, look up by GoType.
func withCodecPathResolution(entries []schema.CodecPathEntry) Option {
	return func(k *Kongfig) {
		if len(entries) == 0 || k.cfg.codecs == nil {
			return
		}
		pathCodecs := make(map[string]anyCodec, len(entries))
		for _, e := range entries {
			if e.CodecName != "" {
				if ac, ok := k.cfg.codecs.byName[e.CodecName]; ok {
					pathCodecs[e.Path] = ac
					continue
				}
				// named codec not registered: fall back to type-based
			}
			if ac, ok := k.cfg.codecs.byType[e.GoType]; ok {
				pathCodecs[e.Path] = ac
			}
		}
		mergeCodecPaths(k, pathCodecs)
	}
}
