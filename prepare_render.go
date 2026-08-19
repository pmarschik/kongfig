package kongfig

import (
	"context"
	"fmt"
	"maps"
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
	// Inherit existing render options from ctx so callers like RenderLayers that
	// call prepareRender on a fresh child Kongfig don't lose settings (TTY size,
	// redacted paths, field names, …) that were established by the parent call.
	ro := renderOptsFromCtx(ctx)
	for _, o := range opts {
		o(&ro)
	}
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
	// The derived order goes under its own key: a caller's explicit order outranks
	// the sort hooks, while what kongfig worked out from the documents and the
	// struct fields is what those hooks reorder.
	_, explicit := readOpts[map[string][]string](ro, RenderKeyOrderKey)
	_, inherited := readOpts[map[string][]string](ro, RenderDerivedKeyOrderKey)
	if !explicit && !inherited {
		kf.mu.RLock()
		docOrder := kf.keyOrderLocked()
		kf.mu.RUnlock()
		if merged := mergeKeyOrders(docOrder, cfg.FieldOrder); merged != nil {
			ro.bind(RenderDerivedKeyOrderKey, merged)
		}
	}
	if _, ok := readOpts[map[string]string](ro, RenderHelpTextsKey); !ok {
		if cfg.HelpTexts != nil {
			// A fresh seen-set per render call, matching WithRenderHelpTexts:
			// each help text is emitted once per call, not once per process.
			seen := make(map[string]bool)
			ro.bind(RenderHelpTextsKey, cfg.HelpTexts)
			ro.bind(RenderHelpTextsSeenKey, &seen)
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
	all := kf.All()
	// Config-path keys are loading infrastructure (file pointers), not config values.
	// Hide them from rendered output so they don't appear in "config show".
	kf.mu.RLock()
	for _, cp := range kf.cfg.configPaths {
		deleteNested(all, strings.Split(cp.Key, "."))
	}
	kf.mu.RUnlock()
	data := wrapRenderData(all, sourceMetas, ro, "")
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

// mergeKeyOrders overlays the key order the documents reported on the order the
// schema declares. A document says what its author wanted read first, so it wins
// for the parents it mentions; the schema supplies the keys no document mentioned,
// which is every key of a map or of a struct inside one, since field order is
// only collected for struct types. Returns nil when neither has anything to say.
func mergeKeyOrders(doc, field map[string][]string) map[string][]string {
	switch {
	case len(doc) == 0:
		return field
	case len(field) == 0:
		return doc
	}
	merged := make(map[string][]string, len(doc)+len(field))
	maps.Copy(merged, doc)
	for parent, keys := range field {
		docKeys, both := merged[parent]
		if !both {
			merged[parent] = keys
			continue
		}
		// Both have a say about this parent: the document orders what it mentioned,
		// and the schema places the rest ahead of the alphabetical fallback.
		seen := make(map[string]bool, len(docKeys))
		for _, k := range docKeys {
			seen[k] = true
		}
		combined := docKeys
		for _, k := range keys {
			if !seen[k] {
				combined = append(combined, k)
			}
		}
		merged[parent] = combined
	}
	return merged
}
