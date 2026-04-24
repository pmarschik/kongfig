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
    │     kongfig.RenderValue(s, v, formatted)
    │       if v is RedactedValue → s.Redacted(v.Display)
    │       else                  → s.String/Number/Bool(formatted)
    │
    │   For each source annotation:
    │     kongfig.RenderSourceAnnotation(src, path, s, opts)
    │       delegates to LayerMeta.RenderAnnotation if meta registered in opts
    │       otherwise falls back to FormatSourceAnnotation + s.Annotation
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
   `RenderAnnotation` without a separate lookup.
2. Redaction state is resolved once; renderers only style it.

`RenderedValue` is ephemeral — created fresh on each render call, never stored in the
internal data map or in layer snapshots. `k.Raw()` and provider snapshots always return
plain Go values. Pre-styling was rejected: each renderer has its own format conventions
(TOML-quoted vs YAML-bare vs JSON-encoded), so styling must happen after format
decisions, not before.

## Styler

`Styler` is a 13-method interface — one method per token class. `BaseStyler` is a no-op embed that implements all methods by returning the input unchanged; custom stylers embed it and override only what they need. The interface (rather than a struct of function fields) lets `style/charming` pre-resolve all lipgloss styles at construction time, keeping zero allocations per render call. Adding a new token class requires updating `style/plain`, `style/charming`, and `mockStyler` in `interfaces_test.go`.

`Styler` is the only dependency renderers take for visual output. Methods fall into three tiers:

**Leaf value styling** — use via `RenderValue` helper, never call directly on leaf values:

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

## RenderValue — mandatory helper

**Never call `s.String/Number/Bool` directly on a leaf value.** Always use:

```go
kongfig.RenderValue(s, v, formattedString)
```

This dispatches to the correct tier method (`s.String`, `s.Number`, `s.Bool`) based on the Go type of `v`, and handles `RedactedValue` centrally. Forgetting this causes redacted values to render as their raw content.

## RenderSourceAnnotation — mandatory helper

**Never format source annotations inline.** Always use:

```go
line += "  " + s.Comment("# ") + kongfig.RenderSourceAnnotation(src, path, s, opts)
```

This delegates to `LayerMeta.RenderAnnotation` when a meta is registered for `src`
(via `LayerMetasKey`), falling back to `FormatSourceAnnotation` + `s.Annotation` for
legacy/simple sources. Using this helper ensures that file paths, env var names, and flag
names are styled with `SourceKind`/`SourceData`/`SourceKey` rather than plain comment color.

## Source filtering in renderers

Before writing a leaf, renderers check:

```go
if len(opts.FilterSource) > 0 && !kongfig.MatchesFilterSource(src, opts.FilterSource) {
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

## RenderOptions

`RenderOptions` is a plain struct assembled at the render call site and passed into `Renderer.Render(w, data, prov, opts)`. Nothing about render options is stored on the `Renderer` itself. This keeps the `Renderer` interface signature stable — new settings are added as fields on `RenderOptions` without changing the interface — and allows the same renderer instance to be used for both `--layers` and the merged view with different options per call.

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
