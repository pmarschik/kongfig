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

## Key order

Nothing about a Go map remembers the order its keys were written in, so renderers get
that order handed to them. `render.OrderedKeys(ctx, prefix, data)` is the single choke
point: it reads the parent-path → ordered-child-names map bound in the context, emits the
keys it names in that order, and appends anything left over alphabetically. Every
renderer and the order-aware `MarshalCtx` funnel through it, so a key order established
once applies to displayed and written output alike.

Five rules can have a say about one parent, and `OrderedKeys` settles them in this order:

1. `WithRenderKeyOrder` for this parent, under `RenderKeyOrderKey` — a caller stating the
   order outright, so the sort hooks below are skipped entirely.
2. `WithRenderKeySort`, a `KeySortFunc` under `RenderKeySortKey`.
3. A `sortby=` mark for this parent, under `KeySortByKey`.
4. The order kongfig derived, under `RenderDerivedKeyOrderKey` — the merge described
   below.
5. Alphabetical, for whatever nothing above placed.

The two derived-order keys are separate for exactly this reason: an explicit order has to
outrank the sort hooks, while the order kongfig worked out on its own is what those hooks
reorder. Both are read by `render.KeyOrder`, explicit first.

`prepareRender` binds the derived map for both the merged view and `--layers`, merging two
sources:

- **The documents.** Each layer records what its parser reported (`KeyOrderParser` /
  `DocumentParser`) in `Layer.KeyOrder`. `Kongfig.KeyOrder()` merges those in pipeline
  order: the first layer to mention a key at a parent path fixes where it reads, a later
  layer that re-states the key does not move it, and keys only a later layer introduces
  follow the ones that do. An override file therefore cannot reshuffle the base file's
  document, and a layer with nothing to say about order — env, flags, structs — subtracts
  nothing.
- **The schema.** `NewFor[T]` collects struct field order into `RenderConfig.FieldOrder`.

Where both have a say about a parent, **the document wins**: it is the one place a human
wrote the keys down in an order they chose. Field order then supplies the keys no
document mentioned, ahead of the alphabetical fallback. That covers the parents field
order cannot reach at all — `schema.FieldOrderPaths` descends only into struct types, so
map keys, and the fields of a struct inside a map, never get an entry. Declaration order
is also the weaker signal in practice: `govet`'s `fieldalignment` autofix reorders struct
fields for packing, which would silently re-sort rendered output that leaned on it.

Two escape hatches sit above the merge:

- `WithRenderKeyOrder(m)` at the call site replaces it outright — the caller has the last
  word.
- `WithLayerKeyOrder(m)` on `LoadParsed` attaches an order to a layer you parsed
  yourself. Providers built on `providers/file.Provider` do this for you.

### Order from a value

Neither source above can order the entries of a map: the keys are data, and nothing in
the schema or the document says which entry matters most. What does say is often a value
inside the entries — a priority, a weight, a rank. A `sortby=` tag names it, and
`WithRenderKeySort` handles what a tag cannot state:

```go
type Config struct {
    Rules map[string]Rule `kongfig:"rules,sortby=-priority"`  // highest first
}

// or, for an order that has to be computed:
kf.RenderWith(ctx, w, r, kongfig.WithRenderKeySort(
    func(path string, keys []string, data kongfig.ConfigData) []string {
        if path != "rules" { return keys }
        return sortByWeight(keys, data)
    }))
```

Both reorder the keys the rules below them produced, so a comparator is handed the
`sortby` order and a `sortby` mark is handed the document's — which is what makes ties
fall back to the document order instead of to map iteration. Values compare within their
kind, and an entry whose value is missing or of another kind reads last in both
directions rather than sorting as a zero. A comparator that drops or invents keys cannot
change which keys are rendered: dropped keys are appended in the order they had, invented
ones ignored.

`sortby` is documented per-tag in [docs/struct-tags.md](struct-tags.md#sortby-option).

## OutputProvider

`OutputProvider` is an optional interface on parsers/providers:

```go
type OutputProvider interface {
    Bind(s Styler) Renderer
}
```

When a parser implements it, `Bind` is called to produce a styled renderer. When it does not, `kongfig.Bind` falls back to a `passthroughRenderer` that marshals via the parser and writes plain bytes without styling. This keeps generic providers (structs, in-memory fixtures) free of rendering obligations while still allowing callers to render any `map[string]any` in any supported format regardless of how it was loaded.

## CtxMarshaler

`Parser.Marshal` sees only the data, which leaves a passthrough render unable to honour anything the render call established — key order first among them. `CtxMarshaler` is the optional way out:

```go
type CtxMarshaler interface {
    MarshalCtx(ctx context.Context, data ConfigData) ([]byte, error)
}
```

`passthroughRenderer` prefers it and falls back to `Marshal`, so implementing it is additive: an existing parser keeps working, and one that adds `MarshalCtx` starts seeing the options — the [key order](#key-order) and its sort hooks, and for TOML the `,inline` marks under `InlineTablesKey`. All three built-in parsers implement it, each with `Marshal` reduced to `MarshalCtx(context.Background(), data)` so the two paths cannot drift. That makes the empty context the plain-write case, and every one of them is byte-for-byte what it wrote before when no order is bound — YAML and JSON both fall back to handing the map to their encoder, whose key sorting is not the plain alphabetical order `render.OrderedKeys` produces. That fast path asks `render.HasKeyOrder(ctx)` rather than reading one key, so a derived order or a sort hook is not silently dropped along with the explicit one.

The same interface makes marshalling order-aware outside rendering entirely. `WithRenderKeyOrderCtx` builds the context, which is what lets a config file be rewritten in the order it was read:

```go
data, order, _ := parser.UnmarshalWithKeyOrder(src)
// … modify data …
out, _ := parser.MarshalCtx(kongfig.WithRenderKeyOrderCtx(ctx, order), data)
```

## TOML layout

The TOML parser renders and writes through the same emitter, so `parsers/toml` layout
options apply to both `Render` and `Marshal`. Construct a configured parser with
`toml.New(opts...)`; `toml.Default` keeps the defaults.

### Indentation

Nested table headers and the keys they own are indented by depth (BurntSushi style):

```toml
[server]
port = 8080
[server.tls]
enabled = true
```

`toml.WithIndent(s)` sets the per-level string. The default is two spaces; `WithIndent("")`
disables indentation and keeps every line flush left.

### Inline tables

A table whose path is marked as inlinable is emitted as a TOML inline table instead of its
own section:

```toml
[buckets]
work = { color = "blue", path = "/w" }
```

Paths are marked either on the parser (`toml.WithInlineTables("buckets.*")`, where a `*`
segment matches exactly one path segment) or from the config struct via the
`kongfig:",inline"` tag — see [struct-tags.md](struct-tags.md#inline-option). Tag-derived
paths reach the parser through the `kongfig.InlineTablesKey` path meta, which `NewFor[T]`
populates; `Parser.MarshalCtx` accepts the same context so writes honour them too.

An inlinable table is only inlined if it passes the gates:

| Gate                     | Render | Marshal |
| ------------------------ | ------ | ------- |
| direct key count ≤ limit | yes    | yes     |
| fits the terminal width  | yes    | no      |

Failing the key-count gate demotes the table to a section. Failing the width gate reflows it
across lines instead — see [Reflowing instead of demoting](#reflowing-instead-of-demoting).

The key limit is `toml.DefaultInlineMaxKeys` (3), overridable globally with
`toml.WithInlineMaxKeys(n)` or per path with `inline=N` in the struct tag (the per-path
value wins when set). Only direct keys count toward the limit — a nested table inside a
candidate is emitted as a nested inline table.

Terminal width is deliberately checked on the render path only: a file written to disk must
not depend on the width of the terminal that happened to produce it.

The width gate is waived for paths marked `toml.WithInlineOverflow("rules")` or tagged
`kongfig:",overflow"`, which reach the parser through the `kongfig.InlineOverflowKey` path
meta. An overflow mark implies an inline one, and it applies to both shapes the width gate
governs: a table keeps its one line, and an array of tables keeps a line per entry instead
of falling back to `[[section]]` blocks.

### Reflowing instead of demoting

An inline table too wide for its line is reflowed rather than demoted to a section: the
brace opens the key's line, one pair follows per line, and the closing brace lines up under
the key. This is a newline inside an inline table, which TOML 1.1 allows.

```toml
[fields]
  blocked_by = {
    jira = "",
    link = {direction = "inward", type = "Blocks"},
    readonly = false,
    type = "issue_links"
  }
```

The last pair takes no trailing comma — TOML 1.0 forbids one in an inline table, and the
reflow keeps every rule but the newline. Pairs are indented one `WithIndent` level past the
key; with `WithIndent("")` they get two spaces, so the shape survives flush-left output.

There is no contest against the block form: the inline mark says the table is an entry
rather than a section, and that reading holds at any width. A marked table reflows whenever
it does not fit, however few pairs it has. Demotion comes only from the key-count gate or
from `toml.WithInlineWrap(false)`.

A value that is itself over the width expands in place, on the same terms it would get on a
line of its own — a nested table reflows, an array becomes a block, a string folds:

```toml
[buckets]
  stormtrooper = {
    dirname = ".",
    match = [
      "github@ixo-corp",
      "github@ixopay-org/*-agent*",
    ],
    nest = true,
    vcs = "jj-colocate"
  }
```

A value that fits is left on its line: expanding it would spend rows to say the same thing.

Provenance rides the opening brace, the way an expanded array's rides its opening bracket —
the comment belongs to the key, and no line of the table can come between the two. Put on
the closing brace instead, the aligner could move it above that line, landing it between
the pairs it annotates. An annotation with several groups that does not fit the opening
line is written on comment lines above the whole entry.

`toml.WithInlineWrap(false)` turns the reflow off for parsers whose rendered output is read
back by a strict TOML 1.0 parser. A marked table that no longer fits then falls back to its
own `[section]`; one that fits is still inlined. The same holds for the elements of a table
array: with no wrapped form to weigh against the block form, an element too wide for the
terminal turns the array into `[[block]]`s rather than spilling a newline into an inline
table.

Like the width gate itself, this is render-only: `Marshal` never reflows.

### Folding long strings

A string value too wide for the terminal is emitted as a multi-line basic string with a
line-ending backslash, which trims the newline and the indentation that follows it:

```toml
description = """the archive keeps every build we shipped, \
  so a restore can start from any release without asking \
  the release team first"""
```

An element of an expanded array folds on the same terms, its continuations indented under
the element and the comma following the closing delimiter:

```toml
remove = [ # file
  """//*[local-name() = 'text' and namespace-uri() = \
    'http://www.w3.org/2000/svg']""",
]
```

The value is unchanged — the document reparses to the same string. Folds happen only at
runs of spaces, so a value with nowhere to break (a long URL, a path) is left on its one
line rather than split mid-token. Redacted values are never folded: the placeholder stands
in for the value, and folding it would fold the placeholder. `Marshal` never folds.

## Bind: parser → renderer

`kongfig.Bind(parser, styler)` wires a `Parser` to a `Styler`:

- If the parser also implements `OutputProvider`, its `Bind(Styler) Renderer` is called — this gives parsers (yaml, toml, json, env) their styled renderers.
- Otherwise a `passthroughRenderer` marshals via `parser.Marshal` and writes plain bytes with no styling.
