# Render Pipeline

This document traces the path from a `Kongfig` instance to terminal output.

## Overview

```
k.Raw()               — merged map[string]any (deep copy, no redaction)
    │
    ▼
ApplyRedaction(data, opts, "")
    │   Walks every leaf path. If path ∈ opts.RedactedPaths and
    │   opts.ShowRedacted == false, replaces the value with:
    │       RedactedValue{Display: opts.RedactFn(path, rawValue)}
    │   Returns data unchanged if ShowRedacted=true or RedactedPaths empty.
    │
    ▼
renderer.Render(w, data, prov, opts)
    │
    │   For each leaf value v:
    │     render.Value(s, v, formatted)
    │       if v is RenderedValue{Redacted:true} → s.Redacted(v.RedactedDisplay)
    │       if v is RenderedValue{Encoded:true}  → s.Codec(formatted)
    │       else                                 → s.String/Number/Bool(formatted)
    │
    │   For each source annotation:
    │     render.Annotation(ctx, rv, path, s)
    │       delegates to LayerMeta.RenderAnnotation if rv.Source has a LayerMeta
    │       returns "" when rv has no source or when render.NoComments is active
    │
    ▼
io.Writer (terminal / file / buffer)
```

## RenderedValue wrapper

`prepareRender` calls `wrapRenderData`, which wraps **every** leaf in
`RenderedValue{Value any, Source SourceMeta, Redacted bool, RedactedDisplay string}`.
For redacted paths, `Redacted` is set to `true` and `RedactedDisplay` is populated from
`opts.RedactFn`. Renderers must never inspect leaves directly — they call
`RenderValue(s, v, formatted)` which handles the type check centrally.

Wrapping all leaves (not just redacted ones) serves two purposes:

1. Source metadata (`SourceMeta`) travels with the value, so renderers can call
   `render.Annotation` without a separate lookup.
2. Redaction state is resolved once; renderers only style it.

`RenderedValue` is ephemeral — created fresh on each render call, never stored in the
internal data map or in layer snapshots. `k.Raw()` and provider snapshots always return
plain Go values. Pre-styling was rejected: each renderer has its own format conventions
(TOML-quoted vs YAML-bare vs JSON-encoded), so styling must happen after format
decisions, not before.

## Styler

`Styler` is a 13-method interface — one method per token class. `render.BaseStyler` is a no-op embed that implements all methods by returning the input unchanged; custom stylers embed it and override only what they need. The interface (rather than a struct of function fields) lets `style/charming` pre-resolve all lipgloss styles at construction time, keeping zero allocations per render call. Adding a new token class requires updating `style/plain`, `style/charming`, and `mockStyler` in `interfaces_test.go`.

`Styler` is the only dependency renderers take for visual output. Methods fall into three tiers:

**Leaf value styling** — use via `render.Value` helper, never call directly on leaf values:

```go
String(s string) string   // string leaf value
Number(s string) string   // int/float leaf value
Bool(s string) string     // boolean leaf value
Redacted(s string) string // redacted placeholder
```

**Structure/comment tokens** — called directly by renderers:

```go
Key(s string) string        // config key name
Comment(s string) string    // comment token (# or //)
BraceOpen(s string) string  // opening bracket ({ [)
BraceClose(s string) string // closing bracket (} ])
```

**Source annotation tokens** — called by `LayerMeta.RenderAnnotation` implementations:

```go
Annotation(source, s string) string // legacy fallback; full styled annotation
SourceKind(s string) string         // source kind ("file", "env", "flags")
SourceData(s string) string         // source data (file path, prefix)
SourceKey(s string) string          // source key ($VAR_NAME, --flag)
```

Two built-in implementations:

- `style/plain`: returns all strings unchanged (no ANSI, safe for piping)
- `style/charming`: lipgloss styles backed by a `theme.Set`; styles resolved once at construction (zero allocation per render)

## render.Value — mandatory helper

**Never call `s.String/Number/Bool` directly on a leaf value.** Always use:

```go
render.Value(s, v, formattedString)
```

This dispatches to the correct tier method (`s.String`, `s.Number`, `s.Bool`) based on the Go type of `v`, and handles `RenderedValue` centrally (redaction, codec styling). Forgetting this causes redacted values to render as their raw content.

## render.Annotation — mandatory helper

**Never format source annotations inline.** Always use:

```go
if ann := render.Annotation(ctx, rv, path, s); ann != "" {
    line += "  " + s.Comment("# ") + ann
}
```

`render.Annotation` returns `""` when `rv` has no source or when `render.NoComments(ctx)` is
true, so callers do not need to check either condition separately.
It delegates to `LayerMeta.RenderAnnotation` (via `rv.Source.Layer`) which handles
structured annotation styling (file paths, env var names, flag names via `SourceKind`/`SourceData`/`SourceKey`).

## Source filtering in renderers

Before writing a leaf, renderers check:

```go
filters := render.FilterSourceFromCtx(ctx)
if !render.MatchesFilterSource(src, filters) {
    continue
}
```

See [Provenance & Filtering](provenance.md) for filter semantics.

## Per-layer rendering (--layers)

`renderPerLayer` in `kong/show` iterates `k.Layers()` and renders each layer's snapshot independently. Each layer uses its own `prov` (a fresh `NewProvenance()`, since the snapshot has no inter-layer provenance). Format is chosen per layer via the layer's `Parser` field if set; otherwise inferred from the source label:

- Source `"flags"` → flags renderer
- Source `"env"` or `"env.*"` → env renderer
- Everything else → YAML (default) or the effective format from `effectiveFormat()`

Empty layers are hidden unless `verbose > 0`, in which case they emit `# === source === (empty)`.

When `verbose == 0`, all `env.*` layers are merged into a single synthetic `"env"` layer before rendering (groupEnvLayers), mirroring how the merged view collapses them.

## Render options via context

Render options are passed as a `context.Context` enriched by `prepareRender`. Renderers read options via the typed key accessors in the `render` sub-package (`render.NoComments(ctx)`, `render.HelpTexts(ctx)`, `render.AlignSources(ctx)`, etc.) rather than inspecting a struct directly. This keeps the `Renderer` interface signature stable — new settings are added as new `RenderOption` keys without changing the interface — and allows the same renderer instance to be used for both `--layers` and the merged view with different options per call.

Call-time options (`[]RenderOption`) are applied in `Kongfig.RenderWith` / `Kongfig.Render` before the `Renderer.Render` call. The root package exports `WithRender*` functions (e.g. `WithRenderNoComments()`, `WithRenderHelpTexts(...)`) that build `RenderOption` values.

## OutputProvider

`OutputProvider` is an optional interface on parsers/providers:

```go
type OutputProvider interface {
    Bind(s Styler) Renderer
}
```

When a parser implements it, `Bind` is called to produce a styled renderer. When it does not, `kongfig.Bind` falls back to a `passthroughRenderer` that marshals via `parser.Marshal` and writes plain bytes without styling. This keeps generic providers (structs, in-memory fixtures) free of rendering obligations while still allowing callers to render any `map[string]any` in any supported format regardless of how it was loaded.

## Bind: parser → renderer

`kongfig.Bind(parser, styler)` wires a `Parser` to a `Styler`:

- If the parser also implements `OutputProvider`, its `Bind(Styler) Renderer` is called — this gives parsers (yaml, toml, json, env) their styled renderers.
- Otherwise a `passthroughRenderer` marshals via `parser.Marshal` and writes plain bytes with no styling.
