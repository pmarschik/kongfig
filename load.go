package kongfig

import (
	"context"
	"errors"
	"maps"
	"time"
)

// LoadOption configures a [Load] call. Use [LoadOptionsKey.Bind] to carry custom
// extension values through to a [Provider.Load] call; use the With* constructors
// for built-in options.
type LoadOption func(*loadOptions)

type loadOptions struct {
	opts         options
	parser       Parser
	providerData ProviderData
	source       string
	silenceKeys  []string
	silenceSet   bool
}

// loadOptionsCtxKey is the context key for the active loadOptions.
type loadOptionsCtxKey struct{}

func withLoadOptionsCtx(ctx context.Context, cfg *loadOptions) context.Context {
	return context.WithValue(ctx, loadOptionsCtxKey{}, cfg)
}

func loadOptionsFromCtx(ctx context.Context) *loadOptions {
	lc, ok := ctx.Value(loadOptionsCtxKey{}).(*loadOptions)
	if !ok {
		return nil
	}
	return lc
}

// WithSource sets the source label for provenance tracking.
func WithSource(name string) LoadOption { return func(c *loadOptions) { c.source = name } }

// WithParser attaches a [Parser] to the layer created by [Kongfig.LoadParsed].
func WithParser(p Parser) LoadOption { return func(c *loadOptions) { c.parser = p } }

// WithProviderData attaches [ProviderData] to the layer created by [Kongfig.LoadParsed].
func WithProviderData(d ProviderData) LoadOption {
	return func(c *loadOptions) { c.providerData = d }
}

// WithSilenceCollisions suppresses env provider collision warnings.
// Pass specific key paths to silence only those; pass none to silence all.
func WithSilenceCollisions(keys ...string) LoadOption {
	return func(c *loadOptions) {
		c.silenceKeys = keys
		c.silenceSet = true
	}
}

// Load calls provider.Load(ctx), applies transforms, merges into the Kongfig,
// and fires OnLoad hooks.
func (k *Kongfig) Load(ctx context.Context, provider Provider, opts ...LoadOption) error {
	// Apply options before provider.Load so extension values are visible to the
	// provider via key.Read(ctx).
	cfg := &loadOptions{}
	for _, o := range opts {
		o(cfg)
	}
	ctx = withLoadOptionsCtx(ctx, cfg)

	data, err := provider.Load(ctx)
	if err != nil {
		return err
	}

	// Normalize any map[string]any sub-trees to ConfigData so all downstream code
	// (collision detection, mergeFrom, applyTransforms) can rely on .(ConfigData) assertions.
	data = normalizeConfigData(data)

	// Source label and kind: WithSource overrides name (kind inferred from override);
	// otherwise use the provider's declared name and kind.
	pi := provider.ProviderInfo()
	source := cfg.source
	kind := pi.Kind
	if source == "" {
		source = pi.Name
	} else {
		kind = inferKind(source)
	}

	// Collision detection: warn if an env.* provider overlaps with existing env.* layers.
	if isEnvSource(source) && !cfg.silenceSet {
		k.mu.RLock()
		existing := k.layers
		k.mu.RUnlock()
		k.warnEnvCollisions(source, data, existing, cfg.silenceKeys)
	}

	// If the provider knows its parser, record it on the layer for native rendering
	// and register it in the known-parsers list for format-based rendering (--format).
	var parser Parser
	if pp, ok := provider.(ParserProvider); ok {
		parser = pp.Parser()
		if parser != nil {
			k.mu.Lock()
			k.registerParsersLocked(parser)
			k.mu.Unlock()
		}
	}

	// Prefer explicit WithProviderData; otherwise ask the provider.
	pd := cfg.providerData
	if pd == nil {
		if pds, ok := provider.(ProviderDataSupport); ok {
			pd = pds.ProviderData()
		}
	}

	// Collect field names for source annotation and format rendering.
	var fieldNames map[string]string
	if pfns, ok := provider.(ProviderFieldNamesSupport); ok {
		fieldNames = pfns.FieldNames()
	}

	// Collect key insertion order from providers that support it (e.g. file provider).
	var keyOrder map[string][]string
	if kop, ok := provider.(KeyOrderProvider); ok {
		keyOrder = kop.KeyOrder()
	}

	// Extract parser format name for LayerMeta.
	format := parserFormat(parser)

	return k.commitLayer(data, source, kind, format, parser, pd, fieldNames, keyOrder)
}

// MustLoad calls Load and panics on error. Useful in init-time setup where
// errors are programmer mistakes rather than runtime conditions.
func (k *Kongfig) MustLoad(ctx context.Context, provider Provider, opts ...LoadOption) {
	if err := k.Load(ctx, provider, opts...); err != nil {
		panic(err)
	}
}

// MustLoadAll calls k.MustLoad for each provider in order.
// Accepts any slice whose element type implements Provider (e.g. []*file.Provider).
// Useful when loading from multiple discovered file providers.
func MustLoadAll[P Provider](ctx context.Context, k *Kongfig, providers []P, opts ...LoadOption) {
	for _, p := range providers {
		k.MustLoad(ctx, p, opts...)
	}
}

// LoadParsed merges pre-parsed data into the Kongfig with the given provenance source label.
//
// This is the low-level entry point for callers that already have a map[string]any
// (e.g. custom file readers, test fixtures, watcher reload callbacks). Transforms
// are applied and OnLoad hooks fire normally; collision detection, convention checks,
// and Provider interface interrogation are skipped.
//
// Options:
//   - [WithParser] — attaches a parser for native-format layer rendering (--layers)
//     and registers it in the Kongfig parser list for --format selection.
//   - [WithProviderData] — attaches [ProviderData] for structured source annotations.
//   - [WithSilenceCollisions] — suppresses env-collision warnings for env.* sources.
func (k *Kongfig) LoadParsed(data ConfigData, source string, opts ...LoadOption) error {
	cfg := &loadOptions{}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.parser != nil {
		k.mu.Lock()
		k.registerParsersLocked(cfg.parser)
		k.mu.Unlock()
	}
	return k.commitLayer(data, source, inferKind(source), parserFormat(cfg.parser), cfg.parser, cfg.providerData, nil, nil)
}

// parserFormat returns the format name from a parser if it implements ParserNamer, else "".
func parserFormat(p Parser) string {
	if p == nil {
		return ""
	}
	if on, ok := p.(ParserNamer); ok {
		return on.Format()
	}
	return ""
}

func (k *Kongfig) commitLayer(data ConfigData, source, kind, format string, parser Parser, pd ProviderData, fieldNames map[string]string, keyOrder map[string][]string) error {
	// Build LayerMeta: stamp ID, name, kind, format and timestamp; store provider data.
	lm := LayerMeta{
		ID:        nextSourceID(),
		Timestamp: time.Now(),
		Name:      source,
		Kind:      kind,
		Format:    format,
		Data:      pd,
	}

	// Register field names (env var or flag names) keyed by the new source ID.
	if len(fieldNames) > 0 {
		k.mu.Lock()
		var existing map[string]map[SourceID]string
		if v, ok := k.cfg.pathMeta[FieldNamesKey].(map[string]map[SourceID]string); ok {
			existing = v
		}
		updated := make(map[string]map[SourceID]string, len(existing)+len(fieldNames))
		maps.Copy(updated, existing)
		for path, name := range fieldNames {
			if updated[path] == nil {
				updated[path] = make(map[SourceID]string)
			}
			updated[path][lm.ID] = name
		}
		k.cfg.pathMeta[FieldNamesKey] = updated
		k.mu.Unlock()
	}

	// Normalize any map[string]any sub-trees (e.g. from LoadParsed callers passing raw data)
	// to ConfigData so all downstream code can rely on .(ConfigData) assertions.
	data = normalizeConfigData(data)

	// Apply registered key renames / deprecation migrations.
	var renameWarnings []string
	var renameErr error
	data, renameWarnings, renameErr = k.applyRenames(data, source, "")
	if renameErr != nil {
		return renameErr
	}

	// Apply bidirectional path codecs (those with an Encode function) at load time
	// so stored values are typed Go values that render correctly.
	// Decode-only codecs run lazily in Get[T] to preserve raw values in the store.
	var err error
	data, err = applyBidirectionalCodecs(k, data)
	if err != nil {
		return err
	}

	sm := SourceMeta{Layer: lm}

	// Build proposed state without touching k.data yet.
	// Hooks run against proposed; only commit if all hooks pass.
	k.mu.Lock()
	proposed := k.data.Clone()
	proposedProv := k.prov.clone()
	delta := make(ConfigData)
	proposed.mergeFrom(data, sm, proposedProv, k.cfg.mergeFuncs, delta, "")
	snapshot := data.Clone()
	layer := Layer{Meta: lm, Data: snapshot, Parser: parser, KeyOrder: keyOrder}
	hooks := make([]LoadFunc, len(k.hooks.onLoad))
	copy(hooks, k.hooks.onLoad)
	k.mu.Unlock()

	// Deep-copy Layer.Data in the event so hook mutations don't alias the
	// snapshot that will be committed into k.layers.
	eventLayer := layer
	eventLayer.Data = layer.Data.Clone()
	event := LoadEvent{Layer: eventLayer, Delta: delta, ProposedData: proposed}
	var errs []error
	for _, h := range hooks {
		if r := h(event); r.Err != nil {
			errs = append(errs, r.Err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	// All hooks passed: commit proposed state and any non-fatal migration warnings.
	k.mu.Lock()
	k.data = proposed
	k.prov = proposedProv
	k.layers = append(k.layers, layer)
	k.cfg.migrationWarnings = append(k.cfg.migrationWarnings, renameWarnings...)
	k.mu.Unlock()

	return nil
}

// DeriveInput is passed to [DeriveFn] by [Kongfig.Derive].
// Additional fields may be added in future minor versions.
type DeriveInput struct {
	Data       ConfigData
	Provenance *Provenance
}

// DeriveOutput is returned by [DeriveFn].
// Additional fields may be added in future minor versions.
type DeriveOutput struct {
	Data ConfigData
}

// DeriveFn is the signature for functions passed to [Kongfig.Derive].
// It receives the current merged configuration and provenance, and returns overlay
// data to be merged with a "derived" source label.
// Returning an error aborts the derive operation.
type DeriveFn func(DeriveInput) (DeriveOutput, error)

// Derive applies a user-supplied function to the current merged state and merges
// the result back with a "derived" source label.
//
// This is a post-load operation for computing values that depend on multiple config
// fields. The function receives the full merged ConfigData and its provenance
// (all providers applied), computes overlay data, and returns it; Kongfig merges
// it as a new layer.
//
// Use this after all providers are loaded:
//
//	k.MustLoad(ctx, provider1)
//	k.MustLoad(ctx, provider2)
//	k.Derive(func(in kongfig.DeriveInput) (kongfig.DeriveOutput, error) {
//	    // example: compute normalized bucket dirnames from map keys and separators
//	    result := make(kongfig.ConfigData)
//	    buckets := in.Data["buckets"].(kongfig.ConfigData)
//	    for k := range buckets {
//	        result[k] = normalize(k, in.Data["separators"]) // your logic
//	    }
//	    return kongfig.DeriveOutput{Data: result}, nil
//	})
//
// Errors from fn cause Derive to return that error without modifying the Kongfig state.
func (k *Kongfig) Derive(fn DeriveFn) error {
	k.mu.RLock()
	current := k.data.Clone()
	prov := k.prov.clone()
	k.mu.RUnlock()

	// Call the derive function with the current merged state and provenance.
	out, err := fn(DeriveInput{Data: current, Provenance: prov})
	if err != nil {
		return err
	}

	// Normalize the result (same normalization as Load does).
	data := normalizeConfigData(out.Data)

	// Build LayerMeta with "derived" source label and kind.
	lm := LayerMeta{
		ID:        nextSourceID(),
		Timestamp: time.Now(),
		Name:      "derived",
		Kind:      KindDerived,
		Format:    "",
		Data:      nil,
	}

	sm := SourceMeta{Layer: lm}

	// Merge derived data into the current state.
	k.mu.Lock()
	proposed := k.data.Clone()
	proposedProv := k.prov.clone()
	delta := make(ConfigData)
	proposed.mergeFrom(data, sm, proposedProv, k.cfg.mergeFuncs, delta, "")
	snapshot := data.Clone()
	layer := Layer{Meta: lm, Data: snapshot, Parser: nil, KeyOrder: nil}
	k.mu.Unlock()

	// Since Derive is a synchronous post-load operation with no hooks (unlike Load),
	// we commit immediately without running OnLoad hooks.
	k.mu.Lock()
	k.data = proposed
	k.prov = proposedProv
	k.layers = append(k.layers, layer)
	k.mu.Unlock()

	return nil
}

// DeriveLoadFn is the signature for functions passed to [Kongfig.DeriveLoad].
// It receives the current merged configuration and provenance, and returns a
// slice of providers to load in order.
// Returning an error aborts the DeriveLoad operation before any providers are loaded.
type DeriveLoadFn func(DeriveInput) ([]Provider, error)

// DeriveLoad calls fn with the current merged state, then loads each returned
// [Provider] in order via [Kongfig.Load].
//
// This bridges the gap between [Derive] (which produces overlay data) and the
// common pattern of computing file paths — or any other provider inputs — from
// the current config and loading them as full layers with their own source labels,
// parsers, and provenance.
//
// Each provider is loaded with opts applied, so callers can pass [WithSource] or
// other options that apply uniformly to all derived providers. Per-provider options
// (e.g. a source label unique to each file) should be embedded in the Provider
// itself or wrapped via a custom Provider implementation.
//
// Use this after base providers are loaded:
//
//	k.MustLoad(ctx, defaults)
//	k.MustLoad(ctx, envProvider)
//	k.DeriveLoad(ctx, func(in kongfig.DeriveInput) ([]kongfig.Provider, error) {
//	    cfg, _ := kongfig.Get[AppConfig](k)
//	    var providers []kongfig.Provider
//	    for _, root := range cfg.Roots {
//	        providers = append(providers, file.Provider(filepath.Join(root, ".app.yaml")))
//	    }
//	    return providers, nil
//	})
//
// Errors from fn, or from any [Kongfig.Load] call, abort the sequence and leave
// the Kongfig in whatever state it was in after the last successful load.
func (k *Kongfig) DeriveLoad(ctx context.Context, fn DeriveLoadFn, opts ...LoadOption) error {
	k.mu.RLock()
	current := k.data.Clone()
	prov := k.prov.clone()
	k.mu.RUnlock()

	providers, err := fn(DeriveInput{Data: current, Provenance: prov})
	if err != nil {
		return err
	}

	for _, p := range providers {
		if err := k.Load(ctx, p, opts...); err != nil {
			return err
		}
	}
	return nil
}
