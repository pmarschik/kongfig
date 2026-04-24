package kongfig

import "context"

// ProviderInfo carries the stable identity of a Provider layer.
type ProviderInfo struct {
	// Name is the stable source label for provenance tracking, collision detection,
	// and --layers grouping (e.g. "env.prefix", "xdg.yaml", "defaults").
	// Env providers must use the "env" prefix; file providers use "<discoverer>.<format>"
	// (e.g. "xdg.yaml", "workdir.toml") with Kind set to KindFile.
	Name string
	// Kind is the provider category. Use the Kind* constants (KindEnv, KindFile, …).
	Kind string
}

// Provider loads configuration as a key-value map.
// ctx carries deadlines and cancellation; implementations that do I/O must respect it.
type Provider interface {
	Load(ctx context.Context) (ConfigData, error)
	ProviderInfo() ProviderInfo
}

// ByteProvider loads raw configuration bytes (e.g. file, HTTP).
// Implement alongside Provider when the raw bytes are meaningful for display.
type ByteProvider interface {
	LoadBytes(ctx context.Context) ([]byte, error)
}

// ParserProvider is an optional Provider extension implemented by providers
// that parse structured data (e.g. file providers). Kongfig calls Parser()
// after Load() and stores the result in [Layer.Parser] so that renderers
// can use it to reproduce the native format (e.g. TOML layer → TOML output).
//
// Implement this on a provider when the provider is tightly coupled to a
// specific parser (e.g. [file.Provider] wraps a [Parser]).
type ParserProvider interface {
	Parser() Parser
}
