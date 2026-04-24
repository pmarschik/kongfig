package kongfig

// OnLoad registers a hook called after each successful Load.
// If any hook returns a LoadResult with a non-nil Err, Load propagates that error.
// All hooks are called regardless; errors are joined via errors.Join.
func (k *Kongfig) OnLoad(fn LoadFunc) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.hooks.onLoad = append(k.hooks.onLoad, fn)
}

// RegisterCodec registers a [Codec] for a specific config path on an existing
// Kongfig instance.
//
// Bidirectional codecs ([Of]) have both Decode and Encode: Decode runs at
// [Get] time; Encode runs at render time (the value is styled with [Styler.Codec]).
//
// Decode-only codecs ([DecodeOnly]) have no Encode: Decode runs only at [Get]
// time, so the raw value is preserved in the store and shown verbatim by renderers.
//
// Use [Of] to wrap a typed [Codec][T] into the required [CodecEntry]:
//
//	kf.RegisterCodec("addr", kongfig.Of(codec.IP))
//	kf.RegisterCodec("timeout", kongfig.Of(codec.Duration))
//
// For decode-only normalization:
//
//	kf.RegisterCodec("tags", kongfig.DecodeOnly(splitComma))
//
// For construction-time registration, prefer [WithCodec] or [WithCodecRegistry].
func (k *Kongfig) RegisterCodec(path string, e CodecEntry) {
	k.mu.Lock()
	defer k.mu.Unlock()
	mergeCodecPaths(k, map[string]anyCodec{path: e.inner})
}

// SetMergeFunc registers a custom merge strategy for the given dot-delimited path.
func (k *Kongfig) SetMergeFunc(path string, fn MergeFunc) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.cfg.mergeFuncs[path] = fn
}
