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
