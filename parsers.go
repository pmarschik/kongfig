package kongfig

import "context"

// Parser encodes and decodes configuration between raw bytes and a map.
type Parser interface {
	Unmarshal([]byte) (ConfigData, error)
	Marshal(ConfigData) ([]byte, error)
}

// ParserNamer is an optional interface for Parser implementations that can
// report their format name and supported file extensions.
// Used by fileprovider.Discover to build source labels and match files.
type ParserNamer interface {
	// Format returns the short format name: "yaml", "toml", "json".
	Format() string
	// Extensions returns the file extensions this parser handles: [".yaml", ".yml"].
	Extensions() []string
}

// CtxMarshaler is an optional interface a [Parser] can implement to marshal
// with the render context in hand. Where [Parser.Marshal] sees only the data,
// MarshalCtx also sees the render options the call established — the key order
// under [RenderKeyOrderKey] above all, so written output can follow the order
// the document was parsed in instead of falling back to alphabetical, and the
// sort hooks that order a map's entries by a value inside them ([KeySortByKey],
// [RenderKeySortKey]).
//
// [Bind] prefers it: a parser that is not an [OutputProvider] is wrapped in a
// renderer that calls MarshalCtx when available and [Parser.Marshal] otherwise.
// Implementations must stay usable without any options in the context, since
// Marshal is still the entry point for plain writes.
type CtxMarshaler interface {
	MarshalCtx(ctx context.Context, data ConfigData) ([]byte, error)
}

// OutputProvider is implemented by providers and parsers that can render
// their own format. Bind returns a Renderer styled with the given Styler.
type OutputProvider interface {
	Bind(s Styler) Renderer
}

// KeyOrderParser is an optional interface a [Parser] can implement to report
// the key insertion order from a parsed document alongside the data.
// [providers/file.Provider] checks for this interface during [Provider.Load] and
// stores the result in [Layer.KeyOrder] for use by [Kongfig.RenderLayers].
type KeyOrderParser interface {
	UnmarshalWithKeyOrder([]byte) (data ConfigData, keyOrder map[string][]string, err error)
}

// DocumentMeta carries the metadata a [DocumentParser] reports alongside the
// decoded data. Both maps may be nil when the parser has nothing to report.
type DocumentMeta struct {
	// KeyOrder maps a parent dot-path ("" for the document root) to that
	// parent's keys in document order, as in [KeyOrderParser].
	KeyOrder map[string][]string
	// Positions maps a config dot-path to where its value starts in the
	// document. Parsers leave [SourcePosition.File] empty — only the provider
	// knows the document's path, and it fills the field in after parsing.
	Positions map[string]SourcePosition
}

// DocumentParser is an optional interface a [Parser] can implement to report
// document metadata (key order and source positions) from a single parse.
// [providers/file.Provider] prefers it over [KeyOrderParser] during
// [Provider.Load] and exposes the positions through [LayerMeta.PositionOf].
//
// Implementing DocumentParser subsumes [KeyOrderParser]; a parser that reports
// both should implement DocumentParser and delegate UnmarshalWithKeyOrder to it.
type DocumentParser interface {
	UnmarshalDocument([]byte) (data ConfigData, meta DocumentMeta, err error)
}

// KeyOrderProvider is an optional interface a [Provider] can implement to report
// the key insertion order captured during its most recent [Provider.Load] call.
// [Kongfig.Load] checks for this interface after loading and stores the result in
// [Layer.KeyOrder] so renderers can preserve file source order in --layers mode.
type KeyOrderProvider interface {
	KeyOrder() map[string][]string
}
