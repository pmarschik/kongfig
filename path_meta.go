package kongfig

// FieldNamesKey is the [PathMetaKey] for per-path provider field names (env var or flag names).
// Each path maps to a [SourceID] → name map. Populated automatically from
// [ProviderFieldNamesSupport] providers; read via [FieldNameFromCtx].
var FieldNamesKey = NewPathMetaKey[map[SourceID]string]()

// SplitSepKey is the [PathMetaKey] for per-path split separators used when parsing
// slice values from env var strings (e.g. "," for "foo,bar,baz" → ["foo","bar","baz"]).
// Populated automatically by [NewFor] from kongfig-sep struct tags, or via [WithSplits].
// The env renderer uses this to rejoin []string values into a single env var string.
var SplitSepKey = NewPathMetaKey[string]()

// MapSplitSpecKey is the [PathMetaKey] for per-path [MapSplitSpec] values used when parsing
// map values from env var strings (e.g. "k1=v1,k2=v2" → map[string]string).
// Populated automatically by [NewFor] from kongfig keysep/sep struct tag options, or via [WithMapSplits].
// The env renderer uses this to rejoin map[string]string values into a single env var string.
var MapSplitSpecKey = NewPathMetaKey[MapSplitSpec]()

// codecPathsKey is the private [PathMetaKey] for path → anyCodec maps stored in pathMeta.
// Set by withCodecPathResolution (called from NewFor[T]); consumed at load time
// (commitLayer) and render time (wrapRenderData).
var codecPathsKey = NewPathMetaKey[anyCodec]()

// mergePathMetaInto copies all entries from src (a cfgState.pathMeta map) into
// the pathMeta container of dst, skipping skipKey (handled separately).
func mergePathMetaInto(dst *renderOptions, src map[any]any, skipKey any) {
	for k, v := range src {
		if k == skipKey {
			continue
		}
		dst.bindInner(pathMetaContainerKey{}, k, v)
	}
}
