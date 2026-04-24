package kongfig

import (
	"context"
	"fmt"
	"strings"
)

// filteredFieldNames applies HideEnvVarNames / HideFlagNames to fn.
// Env var names are those not starting with "--"; flag names start with "--".
// Returns nil when the result is empty.
func filteredFieldNames(fn PathFieldNames, hideEnv, hideFlags bool) PathFieldNames {
	if !hideEnv && !hideFlags {
		return fn
	}
	filtered := make(PathFieldNames)
	for path, sources := range fn {
		for sid, name := range sources {
			isFlag := strings.HasPrefix(name, "--")
			if isFlag && hideFlags {
				continue
			}
			if !isFlag && hideEnv {
				continue
			}
			if filtered[path] == nil {
				filtered[path] = make(map[SourceID]string)
			}
			filtered[path][sid] = name
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

// prepareRender filters, redacts, and wraps kf's data with [RenderedValue] for each leaf.
// Returns the wrapped data and a child ctx with render options injected for renderers.
//
// Renderers should call [RenderAnnotation] for source annotations and
// [RenderValue] for leaf values; both handle the RenderedValue wrapper.
func prepareRender(ctx context.Context, kf *Kongfig, opts ...RenderOption) (ConfigData, context.Context) {
	ro := applyRenderOptions(opts)
	kf.mu.RLock()
	cfg := kf.render
	kf.mu.RUnlock()

	// Apply instance-level render settings as defaults (don't overwrite explicit call opts).
	if _, ok := readOpts[map[string]bool](ro, renderRedactedPathsKey); !ok {
		if cfg.RedactedPaths != nil {
			ro.bind(renderRedactedPathsKey, cfg.RedactedPaths)
		}
	}
	if _, ok := readOpts[func(string, string) string](ro, renderRedactFnKey); !ok {
		if cfg.RedactFn != nil {
			ro.bind(renderRedactFnKey, cfg.RedactFn)
		}
	}

	// Merge registered path meta from Kongfig (e.g. split separators, codec paths from NewFor[T]).
	// codecPathsKey entries flow through here automatically — no special injection needed.
	// FieldNamesKey is skipped here and injected below after filtering.
	kf.mu.RLock()
	kfPathMeta := kf.cfg.pathMeta
	kf.mu.RUnlock()
	mergePathMetaInto(&ro, kfPathMeta, FieldNamesKey)

	// Inject (filtered) field names under FieldNamesKey.
	filtered := filteredFieldNames(kf.FieldNames(), cfg.HideEnvVarNames, cfg.HideFlagNames)
	if filtered != nil {
		ro.bindInner(pathMetaContainerKey{}, FieldNamesKey, map[string]map[SourceID]string(filtered))
	}

	sourceMetas := kf.Provenance().SourceMetas()
	data := wrapRenderData(kf.All(), sourceMetas, ro, "")
	childCtx := withRenderOptsCtx(ctx, ro)
	return data, childCtx
}

// wrapRenderData walks the nested map and wraps each leaf in a RenderedValue.
// Sub-maps are recursed into; empty sub-maps after filtering are omitted.
func wrapRenderData(m ConfigData, sourceMetas map[string]SourceMeta, ro renderOptions, prefix string) ConfigData {
	// Resolve path codecs once per wrapRenderData call (not per leaf).
	var pathCodecs map[string]anyCodec
	if v, ok := ro.readInner(pathMetaContainerKey{}, codecPathsKey); ok {
		if pc, ok := v.(map[string]anyCodec); ok {
			pathCodecs = pc
		}
	}

	filterSource, _ := readOpts[[]string](ro, RenderFilterSourceKey)
	showRedacted, _ := readOpts[bool](ro, renderShowRedactedKey)
	redactedPaths, _ := readOpts[map[string]bool](ro, renderRedactedPathsKey)
	redactFn, _ := readOpts[func(string, string) string](ro, renderRedactFnKey)

	out := make(ConfigData, len(m))
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if sub, ok := v.(ConfigData); ok {
			wrapped := wrapRenderData(sub, sourceMetas, ro, path)
			if len(wrapped) > 0 {
				out[k] = wrapped
			}
			continue
		}
		sm := sourceMetas[path]
		if len(filterSource) > 0 && !matchesFilterLayer(sm.Layer.Kind, sm.Layer.Name, filterSource) {
			continue
		}
		// If a codec with an Encode function is registered for this path, encode the
		// typed value back to its canonical string and mark the RenderedValue so
		// renderers can apply the Codec style instead of String.
		rv := RenderedValue{Value: v, Source: sm}
		if ac, ok := pathCodecs[path]; ok && ac.encode != nil {
			rv.Value = ac.encode(v)
			rv.Encoded = true
		}
		if !showRedacted && redactedPaths[path] {
			fn := redactFn
			if fn == nil {
				fn = func(_, _ string) string { return "<redacted>" }
			}
			rv.Redacted = true
			rv.RedactedDisplay = fn(path, fmt.Sprintf("%v", v))
		}
		out[k] = rv
	}
	return out
}
