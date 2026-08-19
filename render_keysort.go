package kongfig

import "context"

// KeySortFunc reorders the children of one parent path. It is handed the parent
// path (empty for the root), the keys in the order the lower-precedence rules
// produced — an explicit order, the document, the schema, then the alphabet —
// and the parent's data, and returns the keys in the order to render them.
//
// Keys the function drops are appended in their original order and keys it
// invents are ignored, so a comparator cannot change which keys are rendered,
// only where they read.
type KeySortFunc func(path string, keys []string, data ConfigData) []string

// KeySortByKey is the [PathMetaKey] for maps whose entries are ordered by a
// value inside them: map-path → sort spec, a value name optionally dotted to
// reach one level down and optionally prefixed with "-" for descending order.
//
// [NewFor] populates it from sortby= struct tags (see [schema.KeySortByPaths]).
// Renderers consume it through render.OrderedKeys, which applies it below an
// explicit [WithRenderKeyOrder] and a [WithRenderKeySort] comparator, and above
// the order the documents and the schema reported.
var KeySortByKey = NewPathMetaKey[string]()

// Built-in render option keys for value-derived key order.
var (
	// RenderKeySortKey holds a [KeySortFunc] consulted for every parent path.
	// Set via [WithRenderKeySort].
	RenderKeySortKey = NewRenderOptionsKey[KeySortFunc]()
	// RenderDerivedKeyOrderKey holds the parent-path → ordered-child-names map
	// kongfig derives on its own: the order the loaded documents reported,
	// overlaid on the struct field order. [Kongfig.RenderWith] binds it.
	// It is separate from [RenderKeyOrderKey] so a caller's explicit order can
	// outrank the sort hooks while a derived one does not.
	RenderDerivedKeyOrderKey = NewRenderOptionsKey[map[string][]string]()
)

// WithRenderKeySort sets a comparator that orders the children of every parent
// path, for an order no tag can express — by a value's shape, by a lookup, by
// anything the caller can compute:
//
//	kongfig.WithRenderKeySort(func(path string, keys []string, data kongfig.ConfigData) []string {
//	    if path != "rules" {
//	        return keys
//	    }
//	    return sortRulesByWeight(keys, data)
//	})
//
// It outranks a sortby= tag and is itself outranked by an explicit
// [WithRenderKeyOrder] for the same parent, which is a caller saying exactly
// what it wants. Return keys unchanged for the paths it has no opinion about.
func WithRenderKeySort(fn KeySortFunc) RenderOption {
	return RenderKeySortKey.Bind(fn)
}

// WithRenderKeySortCtx returns a context carrying a [KeySortFunc], for callers
// that marshal or render outside [Kongfig.RenderWith] — a [CtxMarshaler] writing
// a config file, or a direct call to a renderer in a test. In production render
// paths, prefer [WithRenderKeySort].
func WithRenderKeySortCtx(ctx context.Context, fn KeySortFunc) context.Context {
	return RenderKeySortKey.WithCtx(ctx, fn)
}

// WithRenderKeySortByCtx returns a context carrying map-path → sort spec entries
// under [KeySortByKey], for callers that marshal or render outside
// [Kongfig.RenderWith]. In production render paths, [NewFor] binds the specs it
// derived from sortby= struct tags on its own.
func WithRenderKeySortByCtx(ctx context.Context, specs map[string]string) context.Context {
	return KeySortByKey.WithCtx(ctx, specs)
}
