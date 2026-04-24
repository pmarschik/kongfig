package kongfig

import (
	"context"
	"strings"
)

// pathMetaContainerKey is the key under which all PathMetaKey entries are nested
// within the renderOptions (options) bag. This keeps per-path metadata separate
// from global render settings.
type pathMetaContainerKey struct{}

// Built-in render option keys. Keys used by sub-packages (e.g. kongfig/render) are
// exported; internal-only keys remain unexported.
// These use the same [NewRenderOptionsKey] facility that library users use for their own keys.
var (
	RenderNoCommentsKey     = NewRenderOptionsKey[bool]()
	renderShowRedactedKey   = NewRenderOptionsKey[bool]()
	RenderFilterSourceKey   = NewRenderOptionsKey[[]string]()
	RenderVerboseSourcesKey = NewRenderOptionsKey[map[string][]string]()
	RenderHelpTextsKey      = NewRenderOptionsKey[map[string]string]()
	RenderFileRawPathsKey   = NewRenderOptionsKey[bool]()
	renderGroupEnvLayersKey = NewRenderOptionsKey[bool]()
	renderFormatKey         = NewRenderOptionsKey[string]()
	RenderNoAlignSourcesKey = NewRenderOptionsKey[bool]()
	renderRedactedPathsKey  = NewRenderOptionsKey[map[string]bool]()
	renderRedactFnKey       = NewRenderOptionsKey[func(string, string) string]()
)

// renderOptions is the options bag for render configuration.
// It is the same type as [options]; all settings are stored as typed key entries.
type renderOptions = options

// RenderOption configures the render methods and renderer behavior.
type RenderOption func(*renderOptions)

// WithRenderNoComments suppresses source annotation comments in rendered output.
func WithRenderNoComments() RenderOption { return RenderNoCommentsKey.Bind(true) }

// WithRenderShowRedacted reveals values that would otherwise be redacted.
func WithRenderShowRedacted() RenderOption { return renderShowRedactedKey.Bind(true) }

// WithRenderFilterSource filters rendered output to sources matching filters.
// Uses the same format as [MatchesFilterSource]: "no-<prefix>" to exclude,
// positive entries form an allowlist.
func WithRenderFilterSource(filters []string) RenderOption {
	return RenderFilterSourceKey.Bind(filters)
}

// WithRenderVerboseSources sets the per-path verbose source list used to expand
// env sub-sources like "[env.tag, env.kong]" in annotations.
func WithRenderVerboseSources(sources map[string][]string) RenderOption {
	return RenderVerboseSourcesKey.Bind(sources)
}

// WithRenderHelpTexts sets per-path help text shown as comments above each key.
func WithRenderHelpTexts(texts map[string]string) RenderOption {
	return RenderHelpTextsKey.Bind(texts)
}

// WithRenderFileRawPaths instructs file source annotations to display the raw
// canonical path instead of the discoverer-formatted display path.
func WithRenderFileRawPaths() RenderOption { return RenderFileRawPathsKey.Bind(true) }

// WithRenderGroupEnvLayers merges all env.* layers into a single "env" layer
// before per-layer iteration in [RenderLayers]. Last-writer-wins within the group.
// Has no effect outside of [RenderLayers].
func WithRenderGroupEnvLayers() RenderOption { return renderGroupEnvLayersKey.Bind(true) }

// WithRenderFormat sets the output format for [Render] and show renderers.
// Format names match [kongfig.ParserNamer.Format]: "yaml", "toml", "json", etc.
// Special values: "env" → shell export, "flags" → --flag=value.
func WithRenderFormat(format string) RenderOption { return renderFormatKey.Bind(format) }

// WithRenderNoAlignSources disables column-alignment of source annotation comments.
// Alignment is on by default; use this to get compact output where each annotation
// follows immediately after its value with no padding.
func WithRenderNoAlignSources() RenderOption { return RenderNoAlignSourcesKey.Bind(true) }

// --- Context storage ---

type renderOptionsKey struct{}

func renderOptsFromCtx(ctx context.Context) renderOptions {
	if ctx == nil {
		return renderOptions{}
	}
	if ro, ok := ctx.Value(renderOptionsKey{}).(renderOptions); ok {
		return ro
	}
	return renderOptions{}
}

func withRenderOptsCtx(ctx context.Context, ro renderOptions) context.Context {
	return context.WithValue(ctx, renderOptionsKey{}, ro)
}

// --- Public read-side accessors (used by ProviderData implementations and renderers) ---

// WithRenderFieldNamesCtx returns a context with [PathFieldNames] injected for use
// in tests or direct calls to [ProviderData.RenderAnnotation] / [LayerMeta.RenderAnnotation].
// In production code, field names are auto-injected from [ProviderFieldNamesSupport] providers.
func WithRenderFieldNamesCtx(ctx context.Context, names PathFieldNames) context.Context {
	return FieldNamesKey.WithCtx(ctx, map[string]map[SourceID]string(names))
}

// WithRenderFileRawPathsCtx returns a context with fileRawPaths=true for use
// in tests or direct calls to ProviderData.RenderAnnotation.
// In production code, prefer [WithRenderFileRawPaths] passed to [Kongfig.RenderWith].
func WithRenderFileRawPathsCtx(ctx context.Context) context.Context {
	return RenderFileRawPathsKey.WithCtx(ctx, true)
}

// WithRenderNoCommentsCtx returns a context with noComments=true for use in tests
// or direct calls to renderer implementations outside [Kongfig.RenderWith].
// In production code, prefer [WithRenderNoComments] passed to [Kongfig.RenderWith].
func WithRenderNoCommentsCtx(ctx context.Context) context.Context {
	return RenderNoCommentsKey.WithCtx(ctx, true)
}

// WithRenderNoAlignSourcesCtx returns a context with alignment disabled, for use
// in tests or direct calls to renderer implementations outside [Kongfig.RenderWith].
// In production code, prefer [WithRenderNoAlignSources] passed to [Kongfig.RenderWith].
func WithRenderNoAlignSourcesCtx(ctx context.Context) context.Context {
	return RenderNoAlignSourcesKey.WithCtx(ctx, true)
}

// WithRenderHelpTextsCtx returns a context with the given help texts set, for use
// in tests or direct calls to renderer implementations outside [Kongfig.RenderWith].
// In production code, prefer [WithRenderHelpTexts] passed to [Kongfig.RenderWith].
func WithRenderHelpTextsCtx(ctx context.Context, texts map[string]string) context.Context {
	return RenderHelpTextsKey.WithCtx(ctx, texts)
}

// RenderFilterSourceFromCtx returns the effective filter source list by merging
// the value stored in ctx with any additional opts. The opts override the ctx value.
// This is used by show.renderPerLayer to read the filter before data wrapping.
func RenderFilterSourceFromCtx(ctx context.Context, opts ...RenderOption) []string {
	ro := renderOptsFromCtx(ctx)
	for _, o := range opts {
		o(&ro)
	}
	v, _ := readOpts[[]string](ro, RenderFilterSourceKey)
	return v
}

// --- Filter helpers ---

// sourceMatchesPrefix reports whether source equals prefix or starts with "prefix.".
func sourceMatchesPrefix(source, prefix string) bool {
	return source == prefix || strings.HasPrefix(source, prefix+".")
}

// matchesFilterLayer reports whether a layer with kind and name passes the filter list.
// Checks kind first, then name, so "file" matches a layer with Kind=KindFile
// regardless of its Name (e.g. "xdg.yaml" after prefix removal).
// Same "no-" exclusion and allowlist semantics as [MatchesFilterSource].
func matchesFilterLayer(kind, name string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	hasAllowlist := false
	for _, f := range filters {
		if len(f) >= 3 && f[:3] == "no-" {
			prefix := f[3:]
			if sourceMatchesPrefix(kind, prefix) || sourceMatchesPrefix(name, prefix) {
				return false
			}
		} else {
			hasAllowlist = true
		}
	}
	if hasAllowlist {
		for _, f := range filters {
			if len(f) < 3 || f[:3] != "no-" {
				if sourceMatchesPrefix(kind, f) || sourceMatchesPrefix(name, f) {
					return true
				}
			}
		}
		return false
	}
	return true
}

// envSourceLabel returns the env source label for a path, collapsing sub-sources
// and expanding to "[env.tag, env.kong]" format when multiple are found in verboseSources.
func envSourceLabel(path string, ro renderOptions) string {
	vs, _ := readOpts[map[string][]string](ro, RenderVerboseSourcesKey)
	sources := vs[path]
	if len(sources) <= 1 {
		return "env"
	}
	var envSrcs []string
	seen := map[string]bool{}
	for _, s := range sources {
		if (s == "env" || strings.HasPrefix(s, "env.")) && !seen[s] {
			envSrcs = append(envSrcs, s)
			seen[s] = true
		}
	}
	if len(envSrcs) <= 1 {
		return "env"
	}
	return "[" + strings.Join(envSrcs, ", ") + "]"
}

// applyRenderOptions applies opts to a zero renderOptions and returns the result.
func applyRenderOptions(opts []RenderOption) renderOptions {
	var ro renderOptions
	for _, o := range opts {
		o(&ro)
	}
	return ro
}
