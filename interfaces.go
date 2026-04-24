package kongfig

import (
	"context"
	"io"
)

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

// WatchEvent is a sealed sum type representing a watch notification.
// Handle with a type switch:
//
//	switch ev := e.(type) {
//	case kongfig.WatchDataEvent:  // successful reload — ev.Data holds new config
//	case kongfig.WatchErrorEvent: // provider/reload error — ev.Err holds the error
//	}
type WatchEvent interface{ watchEvent() }

// WatchDataEvent carries the new config data after a successful reload.
type WatchDataEvent struct{ Data ConfigData }

// WatchErrorEvent carries an error from a watch provider or a reload failure.
type WatchErrorEvent struct{ Err error }

func (WatchDataEvent) watchEvent()  {}
func (WatchErrorEvent) watchEvent() {}

// WatchFunc is the callback type passed to WatchProvider.Watch.
// It is called whenever the watched source changes or encounters an error.
type WatchFunc func(WatchEvent)

// WatchProvider extends Provider with live-reload capability.
// The callback is invoked whenever the source changes.
// Watch must return when ctx is canceled.
type WatchProvider interface {
	Provider
	Watch(ctx context.Context, cb WatchFunc) error
}

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

// Styler renders config tokens with optional terminal styling.
// Implementations may return ANSI-colored strings (charming) or
// the input unchanged (plain). All methods receive the raw token value.
type Styler interface {
	Key(s string) string
	// String styles a string leaf value.
	String(s string) string
	// Number styles a numeric leaf value (int, float).
	Number(s string) string
	// Bool styles a boolean leaf value (true / false).
	Bool(s string) string
	// Null styles a null/nil leaf value.
	Null(s string) string
	// Syntax styles a structural syntax token: brackets, colons, section markers.
	// Covers JSON {}, TOML [section] headers, YAML : and - markers.
	// Renderers use this for punctuation that delimits structure, not values.
	Syntax(s string) string
	Comment(s string) string
	// Annotation styles a token for a specific config source (e.g. "flags", "env", "file").
	// Used for legacy string-only sources; prefer SourceKind + SourceData for new providers.
	Annotation(source, s string) string
	// SourceKind styles the kind token of a source annotation (e.g. "file", "env", "flags").
	SourceKind(s string) string
	// SourceData styles generic data in a source annotation (e.g. a file path).
	SourceData(s string) string
	// SourceKey styles a source-specific key reference in a source annotation
	// (e.g. "$APP_DB_HOST" for env, "--log-level" for flags).
	// Rendered distinctly from SourceKind and SourceData to highlight the specific identifier.
	SourceKey(s string) string
	// Redacted styles a token that represents a hidden/sensitive value.
	Redacted(s string) string
	// Codec styles a leaf value that has been encoded by a registered [Codec].
	// The value shown is the codec's canonical string representation, which may
	// differ from the raw string the provider loaded (e.g. "192.168.0.1" after
	// an IP codec normalises the address). Distinct from [String] so themes can
	// visually distinguish codec-transformed values.
	Codec(s string) string
}

// Renderer writes formatted configuration to w.
// ctx carries render options (injected by [Kongfig.RenderWith]) and cancellation.
// data contains config values, typically pre-processed by [Kongfig.RenderWith] so
// leaf values are [RenderedValue] instances.
type Renderer interface {
	Render(ctx context.Context, w io.Writer, data ConfigData) error
}

// OutputProvider is implemented by providers and parsers that can render
// their own format. Bind returns a Renderer styled with the given Styler.
type OutputProvider interface {
	Bind(s Styler) Renderer
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

// MergeFunc is a custom merge strategy for a specific config path.
// It receives the current destination value and the incoming source value,
// and must return the merged result. Return a non-nil error to fall back
// to last-writer-wins semantics. It must not reference the Kongfig instance.
type MergeFunc func(dst, src any) (any, error)

// GetOption configures behavior of [Get] and [GetWithProvenance].
// Use [Strict] and [At] to construct options.
// GetOption is not publicly extensible; use closures for custom behavior.
type GetOption func(*getOptions)

type getOptions struct {
	opts options
}
