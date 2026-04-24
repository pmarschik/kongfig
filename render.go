package kongfig

import (
	"context"
	"errors"
	"io"
	"maps"
)

// Render writes the merged config to w. It applies render settings from k
// (redacted paths, hide-names flags) and then calls the renderer.
//
// Format selection (in priority order):
//  1. opts with [WithRenderFormat] — explicit per-call format name (e.g. "yaml", "toml").
//  2. [WithDefaultFormat] — instance-level default set at [New] time.
//  3. First registered [OutputProvider] — deterministic when only one file format
//     is loaded; auto-registration order follows [Load] call order.
//
// Parsers are registered automatically when a [ParserProvider] loads a file,
// or explicitly via [WithParsers] or [RegisterParsers].
//
// Returns [ErrNoRenderer] if no matching parser is registered.
func (k *Kongfig) Render(ctx context.Context, w io.Writer, s Styler, opts ...RenderOption) error {
	data, renderCtx := prepareRender(ctx, k, opts...)
	ro := renderOptsFromCtx(renderCtx)

	k.mu.RLock()
	cfg := k.render
	k.mu.RUnlock()
	effectiveFormat, _ := readOpts[string](ro, renderFormatKey)
	if effectiveFormat == "" {
		effectiveFormat = cfg.DefaultFormat
	}

	for _, p := range k.Parsers() {
		namer, ok := p.(ParserNamer)
		if !ok {
			continue
		}
		op, ok := p.(OutputProvider)
		if !ok {
			continue
		}
		if effectiveFormat != "" && namer.Format() != effectiveFormat {
			continue
		}
		return op.Bind(s).Render(renderCtx, w, data)
	}
	return ErrNoRenderer
}

// ErrNoRenderer is returned by [Kongfig.Render] when no suitable parser is registered.
var ErrNoRenderer = errors.New("kongfig: no renderer found; register a parser via WithParsers or load a file with a ParserProvider")

// RenderWith prepares the data (applying opts) and renders it using r.
// It handles the common pattern of prepare + render in a single call.
func (k *Kongfig) RenderWith(ctx context.Context, w io.Writer, r Renderer, opts ...RenderOption) error {
	data, renderCtx := prepareRender(ctx, k, opts...)
	return r.Render(renderCtx, w, data)
}

// RenderLayers calls fn for each source layer that passes the filter.
// Before calling fn, each layer's data is independently wrapped in [RenderedValue]
// and the context is enriched with the resolved render options. The empty-data case
// is passed to fn unchanged; callers may choose to skip it.
//
// When [WithRenderGroupEnvLayers] is set, all env.* layers are merged into a
// single "env" layer (last-writer wins) before iteration.
//
// The filter from [WithRenderFilterSource] is applied here; callers do not need
// to filter layers themselves.
func (k *Kongfig) RenderLayers(ctx context.Context, fn func(ctx context.Context, layer Layer, data ConfigData) error, opts ...RenderOption) error {
	_, enrichedCtx := prepareRender(ctx, k, opts...)
	ro := renderOptsFromCtx(enrichedCtx)

	layers := k.Layers()
	if groupEnv, _ := readOpts[bool](ro, renderGroupEnvLayersKey); groupEnv {
		layers = mergeEnvLayers(layers)
	}

	filterSource, _ := readOpts[[]string](ro, RenderFilterSourceKey)
	for _, layer := range layers {
		if len(filterSource) > 0 && !matchesFilterLayer(layer.Meta.Kind, layer.Meta.Name, filterSource) {
			continue
		}
		layerKf := New()
		_ = layerKf.LoadParsed(layer.Data, layer.Meta.Name) //nolint:errcheck // in-memory data, never errors
		layerData, layerCtx := prepareRender(enrichedCtx, layerKf, WithRenderFilterSource(nil))
		if err := fn(layerCtx, layer, layerData); err != nil {
			return err
		}
	}
	return nil
}

// mergeEnvLayers merges all env.* layers into a single "env" layer (last write wins).
// Non-env layers pass through unchanged; the merged layer is inserted at the position
// of the first env.* layer.
func mergeEnvLayers(layers []Layer) []Layer {
	merged := make(ConfigData)
	firstEnvIdx := -1
	var firstMeta LayerMeta
	for i, l := range layers {
		if l.Meta.Kind != KindEnv {
			continue
		}
		if firstEnvIdx == -1 {
			firstEnvIdx = i
			firstMeta = l.Meta
		}
		maps.Copy(merged, l.Data)
	}
	if firstEnvIdx == -1 {
		return layers
	}
	out := make([]Layer, 0, len(layers))
	for i, l := range layers {
		if l.Meta.Kind == KindEnv {
			if i == firstEnvIdx {
				out = append(out, Layer{Data: merged, Meta: LayerMeta{
					ID:        firstMeta.ID,
					Timestamp: firstMeta.Timestamp,
					Data:      firstMeta.Data,
					Name:      KindEnv,
					Kind:      KindEnv,
				}})
			}
			continue
		}
		out = append(out, l)
	}
	return out
}

// Bind wires a Parser to a Styler and returns a Renderer.
// If p also implements OutputProvider, its Bind method is used directly.
// Otherwise a generic renderer is returned that marshals via p and applies
// no styling (plain text output).
func Bind(p Parser, s Styler) Renderer {
	if op, ok := p.(OutputProvider); ok {
		return op.Bind(s)
	}
	return &passthroughRenderer{p: p}
}

// passthroughRenderer marshals data via the Parser and writes plain bytes.
type passthroughRenderer struct {
	p Parser
}

func (r *passthroughRenderer) Render(_ context.Context, w io.Writer, data ConfigData) error {
	// Unwrap RenderedValues before marshaling — the Parser doesn't know about them.
	unwrapped := unwrapRenderedValues(data)
	b, err := r.p.Marshal(unwrapped)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// unwrapRenderedValues walks the map and replaces RenderedValue wrappers with
// their underlying Value (or RedactedDisplay for redacted entries).
func unwrapRenderedValues(m ConfigData) ConfigData {
	out := make(ConfigData, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case ConfigData:
			out[k] = unwrapRenderedValues(val)
		case RenderedValue:
			if val.Redacted {
				out[k] = val.RedactedDisplay
			} else {
				out[k] = val.Value
			}
		default:
			out[k] = v
		}
	}
	return out
}
