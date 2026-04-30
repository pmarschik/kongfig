package kongfig

import (
	"context"
	"maps"
)

var appNameKey = NewOptionsKey[string]()

// WithAppName injects an application name into ctx for use by components that
// need it (e.g. XDG file discoverers, kong integration).
// Read the stored name with [AppName].
func WithAppName(ctx context.Context, name string) context.Context {
	return appNameKey.With(ctx, name)
}

// AppName returns the app name stored in ctx by [WithAppName], or "" if not set.
func AppName(ctx context.Context) string {
	v, _ := appNameKey.From(ctx)
	return v
}

var configBaseKey = NewOptionsKey[string]()

// WithConfigBase sets the config file basename used by file locators.
// Defaults to "config" when not set (e.g. probes config.yaml, config.toml).
func WithConfigBase(ctx context.Context, base string) context.Context {
	return configBaseKey.With(ctx, base)
}

// ConfigBase returns the config file basename from ctx, defaulting to "config".
func ConfigBase(ctx context.Context) string {
	v, _ := configBaseKey.From(ctx)
	if v == "" {
		return "config"
	}
	return v
}

var hiddenFilesKey = NewOptionsKey[bool]()

// WithHiddenFiles enables probing of dot-prefixed (hidden) appname variants.
// When set, locators also probe .<appname>.<ext> and .<appname>/<configbase>.<ext>.
func WithHiddenFiles(ctx context.Context) context.Context {
	return hiddenFilesKey.With(ctx, true)
}

// HiddenFiles reports whether hidden file variants should be probed.
func HiddenFiles(ctx context.Context) bool {
	v, _ := hiddenFilesKey.From(ctx)
	return v
}

// Strict returns a [GetOption] that makes [Get] fail if any struct field has no matching key.
func Strict() GetOption { return bindGet(getStrictKey, true) }

// At returns a [GetOption] that decodes the sub-tree at the given dot-delimited path.
func At(path string) GetOption { return bindGet(getPathKey, path) }

// WithPathMeta registers per-path typed metadata on the Kongfig instance.
// The entries are injected into the render context by [Kongfig.RenderWith]
// and can be read at render time via [PathMetaKey.Get] and [PathMetaKey.GetAll].
//
// Use this to attach renderer-specific hints derived from struct annotations:
//
//	var SplitSepKey = kongfig.NewPathMetaKey[string]()
//	kf := kongfig.New(kongfig.WithPathMeta(SplitSepKey, map[string]string{"tags": ","}))
func WithPathMeta[T any](key PathMetaKey[T], entries map[string]T) Option {
	return func(k *Kongfig) {
		k.mu.Lock()
		defer k.mu.Unlock()
		if existing, ok := k.cfg.pathMeta[key].(map[string]T); ok {
			maps.Copy(existing, entries)
			k.cfg.pathMeta[key] = existing
		} else {
			k.cfg.pathMeta[key] = maps.Clone(entries)
		}
	}
}

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
