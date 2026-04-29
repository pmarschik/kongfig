package kongfig

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

// KeyOrderProvider is an optional interface a [Provider] can implement to report
// the key insertion order captured during its most recent [Provider.Load] call.
// [Kongfig.Load] checks for this interface after loading and stores the result in
// [Layer.KeyOrder] so renderers can preserve file source order in --layers mode.
type KeyOrderProvider interface {
	KeyOrder() map[string][]string
}
