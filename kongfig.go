package kongfig

import (
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/pmarschik/kongfig/schema"
)

// LoadEvent is passed to OnLoad hooks after each Load call.
// Hooks run before the load is committed; returning a non-nil LoadResult.Err
// rejects the load — k.data and k.layers are left unchanged.
//
// Use ProposedData (not k.All()) to inspect the post-merge state: k.All()
// still returns the pre-load state at hook execution time.
type LoadEvent struct {
	Delta        ConfigData // keys that changed in this load
	ProposedData ConfigData // merged state after this load; do not mutate — changes affect the committed layer
	Layer        Layer      // the layer being loaded; not yet in k.Layers()
}

// LoadResult is returned by OnLoad hooks.
// A non-nil Err causes Load to propagate that error to the caller.
// Future fields may carry non-fatal diagnostics without changing the hook signature.
type LoadResult struct {
	Err error
}

// LoadFunc is the signature for hooks registered via [Kongfig.OnLoad].
// Return a non-nil Err to reject the load; all hooks run and errors are joined.
type LoadFunc func(LoadEvent) LoadResult

// ChangeFunc is the signature for callbacks registered via [Kongfig.OnChange].
// Called after a watched provider reloads data successfully; return value is ignored.
type ChangeFunc func()

// Layer records one loaded source for --layers display.
type Layer struct {
	// Data holds the merged config values for this source layer.
	Data   ConfigData
	Parser Parser
	Meta   LayerMeta
}

// renderConfig holds render-time configuration accumulated from [New] options.
// It is kept separate from call-time [RenderOption] values so that [Kongfig]
// stays free of render concerns beyond accepting and forwarding these settings.
// [Kongfig.RenderWith] applies it automatically.
type renderConfig struct {
	RedactedPaths map[string]bool
	RedactFn      func(path, value string) string
	// DefaultFormat is the format used by [Render] when [WithRenderFormat] is not set
	// and multiple renderable parsers are registered. Set via [WithDefaultFormat].
	DefaultFormat   string
	HideEnvVarNames bool
	HideFlagNames   bool
}

// hookState groups event hooks and watch sources.
type hookState struct {
	validator ConfigValidator
	onLoad    []LoadFunc
	onChange  []ChangeFunc
	watchers  []watchEntry
}

// cfgState groups configuration that governs how data is processed.
type cfgState struct {
	pathMeta    map[any]any
	codecs      *CodecRegistry
	mergeFuncs  map[string]MergeFunc
	configPaths []schema.ConfigPathEntry
	renames     []*renameEntry
}

// Kongfig is a layered configuration container.
// Load providers in order from lowest to highest priority.
// The last writer wins per key.
type Kongfig struct {
	data    ConfigData
	prov    *Provenance
	logger  *slog.Logger
	hooks   hookState
	cfg     cfgState
	render  renderConfig
	layers  []Layer
	parsers []Parser
	mu      sync.RWMutex
}

// ConfigValidator is an optional post-parse validation hook.
// Register via [WithValidator]; called automatically by [Kongfig.Validate].
// Implemented by [validation.Validator] via its AsOption helper.
type ConfigValidator interface {
	ValidateConfig(k *Kongfig) error
}

// WithValidator registers v as the config validator for this Kongfig.
// [Kongfig.Validate] will call v.ValidateConfig; the kong resolver calls it
// automatically after flag parsing so no additional wiring is needed.
func WithValidator(v ConfigValidator) Option {
	return func(k *Kongfig) { k.hooks.validator = v }
}

// Validate runs the registered [ConfigValidator] if any, returning its error.
// Called automatically by the kong resolver; can also be called directly.
func (k *Kongfig) Validate() error {
	if k.hooks.validator == nil {
		return nil
	}
	return k.hooks.validator.ValidateConfig(k)
}

// Option configures a [Kongfig] instance. Pass options to [New] or [NewFor].
// External packages can define their own Options by returning a func(*Kongfig).
type Option func(*Kongfig)

// WithLogger sets the slog logger used for internal warnings (e.g. provider
// naming violations, env collision warnings). Defaults to slog.Default().
func WithLogger(l *slog.Logger) Option { return func(k *Kongfig) { k.logger = l } }

// WithRedacted registers dot-paths whose values should be hidden in rendered output.
// Use [NewFor] to derive paths automatically from struct tags, or [RedactedPaths]
// to compute them explicitly. The paths are stored in render settings and applied
// automatically by [Kongfig.RenderWith].
func WithRedacted(paths map[string]bool) Option {
	return func(k *Kongfig) { k.render.RedactedPaths = paths }
}

// WithRedactionFunc sets a custom function to determine the redacted display
// string for a value. Called with (path, rawValue); return the string to show.
// Default when not set: returns "<redacted>".
func WithRedactionFunc(fn func(path, value string) string) Option {
	return func(k *Kongfig) { k.render.RedactFn = fn }
}

// WithRedactionString returns an Option that replaces every redacted value with s.
func WithRedactionString(s string) Option {
	return WithRedactionFunc(func(_, _ string) string { return s })
}

// WithHideAnnotationNames suppresses both the env var name suffix ("$VAR") and the
// flag name suffix ("--flag") from source annotations in rendered output.
// Shorthand for WithHideEnvVarNames()+WithHideFlagNames().
func WithHideAnnotationNames() Option {
	return func(k *Kongfig) {
		k.render.HideEnvVarNames = true
		k.render.HideFlagNames = true
	}
}

// WithHideEnvVarNames suppresses the "$VAR" suffix from env source annotations.
func WithHideEnvVarNames() Option { return func(k *Kongfig) { k.render.HideEnvVarNames = true } }

// WithHideFlagNames suppresses the "--flag" suffix from flags source annotations.
func WithHideFlagNames() Option { return func(k *Kongfig) { k.render.HideFlagNames = true } }

// WithParsers pre-registers parsers on the Kongfig instance at construction time.
// Accepts multiple parsers: kongfig.WithParsers(yaml, toml).
// Parsers from [ParserProvider] loads are still auto-registered; WithParsers is
// useful when constructing a Kongfig before any Load calls.
func WithParsers(parsers ...Parser) Option {
	return func(k *Kongfig) { k.registerParsersLocked(parsers...) }
}

// WithDefaultFormat returns an [Option] that pins the default output format used by
// [Render] when [WithRenderFormat] is not set. It lets callers pin a preferred
// format without overriding explicit per-call format selection.
//
// Format selection priority in [Render]:
//  1. [WithRenderFormat] — explicit per-call override.
//  2. [WithDefaultFormat] — instance-level default (this option).
//  3. First registered [OutputProvider] — registration-order fallback.
func WithDefaultFormat(format string) Option {
	return func(k *Kongfig) { k.render.DefaultFormat = format }
}

// RegisterParsers records parsers as "known" for format-based rendering (e.g. --format=toml).
// Parsers loaded via a [ParserProvider] are registered automatically; call this to register
// parsers that may not have been used to load any file (e.g. when no config file was found).
// Only parsers implementing [ParserNamer] contribute a named format entry.
func (k *Kongfig) RegisterParsers(parsers ...Parser) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.registerParsersLocked(parsers...)
}

func (k *Kongfig) registerParsersLocked(parsers ...Parser) {
	for _, p := range parsers {
		namer, ok := p.(ParserNamer)
		if !ok {
			continue
		}
		format := namer.Format()
		if format == "" {
			continue
		}
		if k.hasParserFormat(format) {
			continue
		}
		k.parsers = append(k.parsers, p)
	}
}

// Parsers returns all parsers registered with this Kongfig instance,
// either via [RegisterParsers] or auto-registered when a [ParserProvider] was loaded.
// Useful for deriving the --format enum from the parsers the app actually supports.
func (k *Kongfig) Parsers() []Parser {
	k.mu.RLock()
	defer k.mu.RUnlock()
	out := make([]Parser, len(k.parsers))
	copy(out, k.parsers)
	return out
}

// ParserForPath finds the first parser in parsers whose extensions match the extension of path.
// Only parsers implementing [ParserNamer] are considered. Returns an error if none matches.
// Use with [Kongfig.Parsers] to avoid duplicating the extension→parser switch in every app:
//
//	parser, err := kongfig.ParserForPath(configPath, k.Parsers())
func ParserForPath(path string, parsers []Parser) (Parser, error) {
	ext := filepath.Ext(path)
	for _, p := range parsers {
		if namer, ok := p.(ParserNamer); ok {
			if slices.Contains(namer.Extensions(), ext) {
				return p, nil
			}
		}
	}
	return nil, fmt.Errorf("no parser registered for extension %q", ext)
}

// hasParserFormat reports whether a parser with the given format name is already registered.
// Called under lock from registerParsersLocked.
func (k *Kongfig) hasParserFormat(format string) bool {
	for _, p := range k.parsers {
		if n, ok := p.(ParserNamer); ok && n.Format() == format {
			return true
		}
	}
	return false
}

// FieldNames returns a deep copy of the accumulated path → SourceID → field name map.
// Entries are registered automatically when a [ProviderFieldNamesSupport] provider is loaded.
// Returns nil if no providers have registered field names.
func (k *Kongfig) FieldNames() PathFieldNames {
	k.mu.RLock()
	defer k.mu.RUnlock()
	var fn map[string]map[SourceID]string
	if v, ok := k.cfg.pathMeta[FieldNamesKey].(map[string]map[SourceID]string); ok {
		fn = v
	}
	if len(fn) == 0 {
		return nil
	}
	out := make(PathFieldNames, len(fn))
	for path, sources := range fn {
		m := make(map[SourceID]string, len(sources))
		maps.Copy(m, sources)
		out[path] = m
	}
	return out
}

// New returns an empty Kongfig ready for loading.
func New(opts ...Option) *Kongfig {
	return newKongfig(opts)
}

// NewFor returns a Kongfig pre-configured for the config struct T.
// Redacted paths are automatically derived from T's kongfig struct tags,
// so any field tagged "redacted" (or inheriting redaction) is hidden in
// rendered output without any extra call.
//
// Additional options (e.g. [WithLogger], [WithRedactionFunc]) may be passed;
// they are applied after the auto-derived redacted paths, so [WithRedacted]
// can be used to extend or override the derived set.
//
//	kf := kongfig.NewFor[AppConfig]()
func NewFor[T any](opts ...Option) *Kongfig {
	paths := schema.RedactedPaths[T]()
	if len(paths) > 0 {
		opts = append([]Option{WithRedacted(paths)}, opts...)
	}
	splits := schema.SplitPaths[T]()
	if len(splits) > 0 {
		opts = append([]Option{WithSplits(splits)}, opts...)
	}
	mapSplits := schema.MapSplitPaths[T]()
	if len(mapSplits) > 0 {
		opts = append([]Option{WithMapSplits(mapSplits)}, opts...)
	}
	cfgPaths := schema.ConfigPaths[T]()
	if len(cfgPaths) > 0 {
		opts = append([]Option{withConfigPaths(cfgPaths)}, opts...)
	}
	// Append codec resolution AFTER user opts so WithCodec registrations are visible.
	codecEntries := schema.CodecPaths[T]()
	if len(codecEntries) > 0 {
		opts = append(opts, withCodecPathResolution(codecEntries))
	}
	return newKongfig(opts)
}

func newKongfig(opts []Option) *Kongfig {
	k := &Kongfig{
		data: make(ConfigData),
		prov: NewProvenance(),
		cfg: cfgState{
			mergeFuncs: make(map[string]MergeFunc),
			codecs:     NewCodecRegistry(),
			pathMeta:   make(map[any]any),
		},
	}
	for _, o := range opts {
		o(k)
	}
	return k
}

// log returns the configured logger, falling back to slog.Default().
func (k *Kongfig) log() *slog.Logger {
	if k.logger != nil {
		return k.logger
	}
	return slog.Default()
}

// warnEnvCollisions logs a warning for each key path in incoming that is already
// set by an existing env.* layer with a different value, unless that key is in silenceKeys.
func (k *Kongfig) warnEnvCollisions(source string, incoming ConfigData, existing []Layer, silenceKeys []string) {
	silenced := make(map[string]bool, len(silenceKeys))
	for _, key := range silenceKeys {
		silenced[key] = true
	}

	incomingValues := incoming.FlatValues()
	for _, layer := range existing {
		if !isEnvSource(layer.Meta.Name) {
			continue
		}
		existingValues := layer.Data.FlatValues()
		for path, incomingVal := range incomingValues {
			existingVal, exists := existingValues[path]
			if !exists {
				continue
			}
			if len(silenceKeys) > 0 && silenced[path] {
				continue
			}
			if fmt.Sprint(existingVal) == fmt.Sprint(incomingVal) {
				continue
			}
			k.log().Warn("env provider collision: key already set by another env provider with a different value",
				slog.String("key", path),
				slog.String("existing_source", layer.Meta.Name),
				slog.String("new_source", source),
				slog.String("hint", "use WithSilenceCollisions to suppress; check load order if unintentional"),
			)
		}
	}
}

// isEnvSource reports whether a source label belongs to the env group.
func isEnvSource(source string) bool {
	return source == "env" || strings.HasPrefix(source, "env.")
}
