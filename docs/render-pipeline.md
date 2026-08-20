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
For a redacted path, `wrapRenderData` sets `Redacted` to `true` and takes
`RedactedDisplay` from `opts.RedactFn`. Renderers must never inspect leaves directly. They
call `RenderValue(s, v, formatted)`, which handles the type check centrally.

The wrap covers all leaves, not only the redacted ones, for two reasons:

1. Source metadata (`SourceMeta`) travels with the value, so renderers can call
   `render.Annotation` with no separate lookup.
2. Kongfig resolves the redaction state once, and renderers only style it.

`RenderedValue` is ephemeral. Kongfig creates it new on each render call, and never stores
it in the internal data map or in a layer snapshot. `k.Raw()` and provider snapshots always
return plain Go values. Kongfig rejects pre-styling, because each renderer has its own
format conventions (TOML-quoted, YAML-bare, JSON-encoded). Styling must therefore happen
after the format decisions, not before.

## Styler

`Styler` is a 13-method interface, with one method per token class. `render.BaseStyler` is a no-op embed. It implements every method and returns the input unchanged. A custom styler embeds it and overrides only the methods that it needs.

The design uses an interface instead of a struct of function fields, so `style/charming` can pre-resolve all lipgloss styles at construction time. A render call then costs zero allocations. When you add a new token class, update `style/plain`, `style/charming`, and `mockStyler` in `interfaces_test.go`.

`Styler` is the only dependency that renderers take for visual output. It has three tiers of methods:

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
- `style/charming`: lipgloss styles backed by a `theme.Set`. It resolves the styles once at construction (zero allocation per render)

## render.Value — mandatory helper

**Never call `s.String/Number/Bool` directly on a leaf value.** Always use:

```go
render.Value(s, v, formattedString)
```

This helper dispatches to the correct tier method (`s.String`, `s.Number`, `s.Bool`) for the Go type of `v`, and it handles `RenderedValue` centrally (redaction, codec styling). If you forget it, a redacted value renders as its raw content.

## render.Annotation — mandatory helper

**Never format source annotations inline.** Always use:

```go
if ann := render.Annotation(ctx, rv, path, s); ann != "" {
    line += "  " + s.Comment("# ") + ann
}
```

`render.Annotation` returns `""` in two cases. The first case is an `rv` with no source.
The second case is a true `render.NoProvenance(ctx)`, which covers both
`WithRenderNoProvenance()` and the broader `WithRenderNoComments()`. A caller therefore
tests neither condition itself. The function delegates to
`LayerMeta.RenderAnnotation` (through `rv.Source.Layer`). That method styles the file
path, the env var name and the flag name through `SourceKind`, `SourceData` and
`SourceKey`.

Help comments have the mirror gate. `render.HelpText` and `render.HelpTexts` stay
silent under `WithRenderNoHelp()`. A renderer that uses them therefore honors every
comment flag.

## Source filtering in renderers

A renderer runs this check before it writes a leaf:

```go
filters := render.FilterSourceFromCtx(ctx)
if !render.MatchesFilterSource(src, filters) {
    continue
}
```

See [Provenance & Filtering](provenance.md) for filter semantics.

## Per-layer rendering (--layers)

`renderPerLayer` in `kong/show` iterates `k.Layers()` and renders the snapshot of each layer independently. Each layer uses its own `prov` (a fresh `NewProvenance()`, because the snapshot has no inter-layer provenance). The `Parser` field of the layer selects the format. If that field is empty, the source label selects it:

- Source `"flags"` → flags renderer
- Source `"env"` or `"env.*"` → env renderer
- Everything else → YAML (default) or the effective format from `effectiveFormat()`

Kongfig hides an empty layer. With `verbose > 0`, the layer emits `# === source === (empty)` instead.

When `verbose == 0`, `groupEnvLayers` merges all `env.*` layers into one synthetic `"env"` layer before the render. The merged view collapses them the same way.

## Render options via context

`prepareRender` enriches a `context.Context`, and that context carries the render options. A renderer reads an option through a typed key accessor in the `render` sub-package, and not from a struct. The accessors are `render.NoComments(ctx)`, `render.HelpTexts(ctx)`, `render.AlignSources(ctx)` and more. This design keeps the signature of the `Renderer` interface stable, because a new setting is a new `RenderOption` key. It also lets one renderer instance serve both `--layers` and the merged view, with different options per call.

`Kongfig.RenderWith` and `Kongfig.Render` apply the call-time options (`[]RenderOption`) before the `Renderer.Render` call. The root package exports `WithRender*` functions that build `RenderOption` values, for example `WithRenderNoComments()` and `WithRenderHelpTexts(...)`.

## Key order

A Go map does not remember the order of its keys, so kongfig gives that order to the
renderers. `render.OrderedKeys(ctx, prefix, data)` is the single choke point. It reads the
map of parent-path → ordered-child-names from the context. It emits the keys of that map
in the given order, and it appends the rest alphabetically. Every renderer and the
order-aware `MarshalCtx` pass through this function, so one key order applies to displayed
output and written output alike.

Five rules can have a say about one parent, and `OrderedKeys` settles them in this order:

1. `WithRenderKeyOrder` for this parent, under `RenderKeyOrderKey`. The caller states the
   order outright, so `OrderedKeys` skips the sort hooks below.
2. `WithRenderKeySort`, a `KeySortFunc` under `RenderKeySortKey`.
3. A `sortby=` mark for this parent, under `KeySortByKey`.
4. The order that kongfig derived, under `RenderDerivedKeyOrderKey` — the merge below.
5. Alphabetical order, for every key that no rule above places.

The two derived-order keys are separate for this reason. An explicit order must outrank
the sort hooks. The order that kongfig derived is the order that those hooks reorder.
`render.KeyOrder` reads both keys, and it reads the explicit one first.

`prepareRender` binds the derived map for the merged view and for `--layers`. It merges
two sources:

- **The documents.** Each layer records the report of its parser (`KeyOrderParser` or
  `DocumentParser`) in `Layer.KeyOrder`. `Kongfig.KeyOrder()` merges those reports in
  pipeline order. The first layer that mentions a key at a parent path fixes the position
  of that key. A later layer that re-states the key does not move it, and a key that only
  a later layer introduces comes after the earlier keys. An override file therefore cannot
  reshuffle the document of the base file. A layer with no opinion about order — env,
  flags, structs — removes nothing.
- **The schema.** `NewFor[T]` collects struct field order into `RenderConfig.FieldOrder`.

Where both sources have an opinion about a parent, **the document wins**. A document is
the one place where a human wrote the keys in a chosen order. Field order then supplies
the keys that no document mentions, before the alphabetical fallback.

Field order cannot reach every parent. `schema.FieldOrderPaths` descends only into struct
types. Map keys therefore never get an entry, and neither do the fields of a struct inside
a map. Declaration order is also the weaker signal in practice. The `fieldalignment`
autofix of `govet` reorders struct fields for packing, and that reorders rendered output
with no message.

Two escape hatches sit above the merge:

- `WithRenderKeyOrder(m)` at the call site replaces it outright — the caller has the last
  word.
- `WithLayerKeyOrder(m)` on `LoadParsed` attaches an order to a layer you parsed
  yourself. Providers built on `providers/file.Provider` do this for you.

### Order from a value

Neither source above can order the entries of a map. The keys are data, and nothing in the
schema or the document says which entry matters most. A value inside the entries often
says it — a priority, a weight, a rank. A `sortby=` tag names that value, and
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

Both hooks reorder the keys that the rules below them produced. A comparator therefore
receives the `sortby` order, and a `sortby` mark receives the document order. A tie
therefore uses the document order instead of the map iteration order.

Values compare within their kind. An entry whose value is missing, or of another kind,
reads last in both directions, and does not sort as a zero. A comparator that removes or
invents keys cannot change which keys a render writes. Kongfig appends a removed key in
its earlier order, and ignores an invented one.

`sortby` is documented per-tag in [docs/struct-tags.md](struct-tags.md#sortby-option).

## OutputProvider

`OutputProvider` is an optional interface on parsers/providers:

```go
type OutputProvider interface {
    Bind(s Styler) Renderer
}
```

When a parser implements it, `kongfig.Bind` calls `Bind` to produce a styled renderer. When a parser does not implement it, `kongfig.Bind` uses a `passthroughRenderer` instead. That renderer marshals through the parser and writes plain bytes with no styling. Generic providers, such as structs and in-memory fixtures, therefore carry no render obligations. A caller can still render any `map[string]any` in any supported format, whatever the load path was.

## CtxMarshaler

`Parser.Marshal` sees only the data. A passthrough render therefore cannot honor anything that the render call established, and key order is the first example. `CtxMarshaler` is the optional solution:

```go
type CtxMarshaler interface {
    MarshalCtx(ctx context.Context, data ConfigData) ([]byte, error)
}
```

`passthroughRenderer` prefers `MarshalCtx` and uses `Marshal` otherwise, so the interface is additive. An existing parser keeps working, and a parser that adds `MarshalCtx` starts to see the options. Those options are the [key order](#key-order) with its sort hooks, and the `,inline` marks under `InlineTablesKey`, which TOML and YAML both act on. All three built-in parsers implement the interface, and each one reduces `Marshal` to `MarshalCtx(context.Background(), data)`, so the two paths cannot drift.

The empty context is therefore the plain-write case. With no order bound, every one of those parsers writes byte-for-byte what it wrote before. YAML and JSON both hand the map to their own encoder, whose key sorting is not the plain alphabetical order of `render.OrderedKeys`. That fast path asks `render.HasKeyOrder(ctx)` instead of one key, so kongfig does not drop a derived order or a sort hook with the explicit one.

The same interface makes a marshal order-aware outside a render. `WithRenderKeyOrderCtx` builds the context, so a program can rewrite a config file in the order that it read:

```go
data, order, _ := parser.UnmarshalWithKeyOrder(src)
// … modify data …
out, _ := parser.MarshalCtx(kongfig.WithRenderKeyOrderCtx(ctx, order), data)
```

## TOML layout

The TOML parser renders and writes through the same emitter, so `parsers/toml` layout
options apply to both `Render` and `Marshal`. Construct a configured parser with
`toml.New(opts...)`. The `toml.Default` parser keeps the defaults.

### Indentation

The emitter indents a nested table header and the keys that it owns by depth (BurntSushi
style):

```toml
[server]
port = 8080
[server.tls]
enabled = true
```

`toml.WithIndent(s)` sets the per-level string. The default is two spaces. `WithIndent("")`
turns the indentation off and keeps every line flush left.

### Inline tables

A table whose path carries an inline mark becomes a TOML inline table instead of its own
section:

```toml
[buckets]
work = { color = "blue", path = "/w" }
```

A mark comes from one of two places. The first is the parser option
`toml.WithInlineTables("buckets.*")`, where a `*` segment matches exactly one path segment.
The second is the `kongfig:",inline"` tag on the config struct
([struct-tags.md](struct-tags.md#inline-option)).

A tag-derived path reaches the parser through the `kongfig.InlineTablesKey` path meta, which
`NewFor[T]` populates. `Parser.MarshalCtx` accepts the same context, so a write honors these
paths too. The mark is not TOML-only. YAML reads the same paths and collapses them into flow
mappings ([YAML layout](#yaml-layout)).

A marked table becomes inline only when it passes the gates:

| Gate                     | Render | Marshal |
| ------------------------ | ------ | ------- |
| direct key count ≤ limit | yes    | yes     |
| fits the terminal width  | yes    | no      |

A table that fails the key-count gate becomes a section. A table that fails the width gate
reflows across lines instead
([Reflowing instead of demoting](#reflowing-instead-of-demoting)).

The key limit is `toml.DefaultInlineMaxKeys` (3). `toml.WithInlineMaxKeys(n)` changes it
globally, and `inline=N` in the struct tag changes it for one path. The per-path value wins
when it is set. Only the direct keys count toward the limit, because a nested table inside a
candidate becomes a nested inline table.

Kongfig reads the terminal width on the render path only. A file on disk must not depend on
the width of the terminal that produced it.

An overflow mark waives the width gate. The mark comes from
`toml.WithInlineOverflow("rules")`, or from a `kongfig:",overflow"` tag that reaches the
parser through the `kongfig.InlineOverflowKey` path meta. An overflow mark also implies an
inline one. It applies to both shapes that the width gate governs. A table keeps its one
line, and an array of tables keeps one line per entry instead of `[[section]]` blocks.

### Reflowing instead of demoting

An inline table too wide for its line reflows instead of becoming a section. The brace opens
the line of the key. One pair follows per line. The closing brace aligns under the key. This
shape is a newline inside an inline table, which TOML 1.1 allows.

```toml
[fields]
  blocked_by = {
    jira = "",
    link = {direction = "inward", type = "Blocks"},
    readonly = false,
    type = "issue_links"
  }
```

The last pair takes no trailing comma. TOML 1.0 forbids one in an inline table, and the
reflow keeps every rule except the newline. The pairs sit one `WithIndent` level past the
key. With `WithIndent("")` they get two spaces, so the shape survives flush-left output.

The block form is not a contender here. The inline mark says that the table is an entry and
not a section, and that reading holds at any width. A marked table reflows whenever it does
not fit, however few pairs it has. Only the key-count gate or `toml.WithInlineWrap(false)`
turns the table back into a section.

A value that is itself over the width expands in place, on the same terms as a value on a
line of its own. A nested table reflows, an array becomes a block, and a string folds:

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

A value that fits keeps its line. An expansion of it spends rows to say the same thing.

Provenance rides the opening brace, the way the provenance of an expanded array rides its
opening bracket. The comment belongs to the key, and no line of the table can come between
the two. On the closing brace instead, the aligner can move the comment above that line,
into the middle of the pairs that it annotates. An annotation with several groups that does
not fit the opening line goes on comment lines above the whole entry.

`toml.WithInlineWrap(false)` turns the reflow off. This option fits a parser whose rendered
output a strict TOML 1.0 parser reads back. A marked table that no longer fits then gets its
own `[section]`. A table that fits is still inline. The same holds for the elements of a
table array. With no wrapped form to weigh against the block form, an element too wide for
the terminal turns the array into `[[block]]`s.

Like the width gate itself, the reflow is render-only. `Marshal` never reflows.

### Folding long strings

A string value too wide for the terminal becomes a multi-line basic string with a
line-ending backslash. That backslash trims the newline and the indentation after it:

```toml
description = """the archive keeps every build we shipped, \
  so a restore can start from any release without asking \
  the release team first"""
```

An element of an expanded array folds on the same terms. The continuation lines sit under
the element, and the comma follows the closing delimiter:

```toml
remove = [ # file
  """//*[local-name() = 'text' and namespace-uri() = \
    'http://www.w3.org/2000/svg']""",
]
```

The value is unchanged, and the document reparses to the same string. A fold happens only at
a run of spaces. A value with nowhere to break (a long URL, a path) therefore keeps its one
line instead of a split mid-token. Kongfig never folds a redacted value, because the
placeholder stands for the value, and a fold of it folds the placeholder. `Marshal` never
folds.

A fold leaves room for the provenance of the entry. Past a line-ending backslash, a `#` is
string content and not a comment. The closing `"""` line is therefore the only line of a
folded value that a comment can follow. A comment above that line lands inside the string.

The fold reserves that room on the closing line. The reserved width is the width of the
annotation for a leaf, or the comma of an element in an expanded array. When greedy packing
overshoots the room, the closing chunk moves to a line of its own. A line that cannot carry
the comment is the worse home for it.
[`render.AnnMarkerFixed`](render.md#renderannmarkerfixed) then pins the annotation there, so
the aligner cannot lift it.

A value with nowhere to break keeps its one long line. Its annotation goes on a comment line
above the whole entry, outside the string, where a comment is a comment.

The elements of an expanded array carry a second hold-back. The annotation rides the opening
bracket, so kongfig packs every line below it clear of the column that the annotation aligns
on. The block then reads as one entry with a comment beside it. Kongfig drops that hold-back
in two cases: when it takes more than half the line, or when it leaves too little room to
fold into. A value that the reader can follow is worth more than a clear comment column.

## YAML layout

Like TOML, the YAML parser renders and writes through settings that apply to both
`Render` and `Marshal`. Construct a configured parser with `yaml.New(opts...)`. The
`yaml.Default` parser keeps the defaults.

### Flow mappings

A mapping whose path carries a mark becomes a YAML flow mapping instead of a nested block
mapping:

```yaml
buckets:
  work: { color: blue, path: /w }
```

The marks are the ones that TOML reads for [inline tables](#inline-tables). The parser
options are `yaml.WithInlineMaps("buckets.*")` and `yaml.WithInlineOverflow("buckets.*")`.
The struct tags are `kongfig:",inline"` and `kongfig:",overflow"`, and they reach the parser
through the `kongfig.InlineTablesKey` and `kongfig.InlineOverflowKey` path meta. The gates
are the same too:

| Gate                     | Render | Marshal |
| ------------------------ | ------ | ------- |
| direct key count ≤ limit | yes    | yes     |
| fits the terminal width  | yes    | no      |

The key limit is `yaml.DefaultInlineMaxKeys` (3). `yaml.WithInlineMaxKeys(n)` changes it
globally, and `inline=N` in the struct tag changes it for one path. An
[omitempty](struct-tags.md#omitempty-option) mark removes a key from the output, so that key
does not count toward the limit either.

A mapping that fails either gate keeps the block mapping. YAML differs from TOML at this
point. YAML has no reflow, because a flow mapping across several lines reads no better than
the block mapping that it came from. An overflow mark waives the width gate. `Marshal` never
reads the width at all, because a written file must not depend on the terminal that produced
it.

The values inside a collapsed mapping keep their treatment. A redacted value stays its
placeholder. A nested mapping collapses with its parent, because a block mapping cannot live
inside a flow mapping. The provenance of the values whose lines are gone moves onto the line
that replaced them. One comment covers them when they share a source. Otherwise one group
per source names the keys that it covers.

## Bind: parser → renderer

`kongfig.Bind(parser, styler)` wires a `Parser` to a `Styler`:

- If the parser also implements `OutputProvider`, `Bind` calls its `Bind(Styler) Renderer` method. That method gives each parser (yaml, toml, json, env) its styled renderer.
- Otherwise a `passthroughRenderer` marshals through `parser.Marshal` and writes plain bytes with no styling.
