<!-- read_when: implementing a renderer, adding render options, or reading option values from context -->

# render — Renderer Utilities

The `render` sub-package (`github.com/pmarschik/kongfig/render`) provides shared
utilities that **all renderer implementations** should import. It is the canonical home
for:

- Value styling helpers (`Value`, `Annotation`)
- Annotation alignment (`AlignAnnotations`, `AnnMarker`)
- A no-op `BaseStyler` embed
- Context accessors for render options
- Filter helpers (`MatchesFilterSource`, `BuildFilterSource`)

Sub-packages that implement `kongfig.Renderer` must import `render` rather than
re-implementing these helpers.

---

## Value styling

### `render.Value(s Styler, v any, formatted string) string`

Renders a leaf value using the appropriate `Styler` method. Always call this instead
of `s.String(formatted)` directly on leaf values:

```go
line := render.Value(s, v, formatted)
```

Handles:

- `RenderedValue{Redacted: true}` → `s.Redacted(rv.RedactedDisplay)`
- `RenderedValue{Encoded: true}` → `s.Codec(formatted)`
- `int`/`float*` → `s.Number(formatted)`
- `bool` → `s.Bool(formatted)`
- `nil` → `s.Null(formatted)`
- everything else → `s.String(formatted)`

### `render.Annotation(ctx context.Context, rv RenderedValue, path string, s Styler) string`

Renders the source annotation for a `RenderedValue`. Returns `""` when:

- `rv.Source` is zero (value has no provenance)
- `render.NoProvenance(ctx)` is true — set by `WithRenderNoProvenance()` or the
  broader `WithRenderNoComments()`

Always use this instead of formatting annotations inline:

```go
if ann := render.Annotation(ctx, rv, path, s); ann != "" {
    line += "  " + s.Comment("# ") + ann
}
```

---

## Annotation alignment

### `render.AnnMarker`

A sentinel byte (`\x00`) embedded between a rendered value and its annotation to enable
two-pass column alignment. Insert it during the first render pass:

```go
if align {
    line += render.AnnMarker + "  " + s.Comment("# ") + ann
} else {
    line += "  " + s.Comment("# ") + ann
}
```

### `render.AnnMarkerFixed`

A second sentinel byte (`\x01`) for a line whose annotation must stay on it, because the
line above is not a place a comment may go. The line that closes a folded TOML multi-line
string is one: a comment written above it lands inside the string, and the value the reader
parses back is not the value that was rendered.

A fixed annotation aligns with the others when the alignment column has room for it, and
follows its content directly when it does not. It is never lifted to a line of its own, and
it never pulls the alignment column: only movable annotations get a say in where that
column lands.

Use it only where the above-line fallback would change the meaning of the output — a plain
`AnnMarker` is the right choice everywhere else.

### `render.AlignAnnotations(raw string, w io.Writer) error`

Post-processes lines containing `AnnMarker`, padding each value segment to the same
column before its annotation. Lines without the marker are written as-is.

```go
var buf bytes.Buffer
if err := renderMap(ctx, &buf, s, data, true); err != nil {
    return err
}
return render.AlignAnnotations(buf.String(), w)
```

### `render.AlignAnnotationsCtx(ctx context.Context, raw string, w io.Writer) error`

Like `AlignAnnotations` but reads the terminal width from `ctx` (set via `render.WithTTYSize`
or `render.TTYSizeKey`). When a line cannot fit its annotation inline, that annotation is
written as a comment line **above** the value line, indented to match:

```
# from file.yaml
host: localhost
# from environment
port: 8080
```

The decision is per line, not per document. The alignment column is chosen to keep as many
annotations inline as it can, so one unusually wide line gives way on its own instead of
pushing every annotation in the document above its value:

```
host = "localhost"                  # from file.yaml
port = 8080                         # from environment
# defaults
endpoint = "https://…very long…"
```

Prefer `AlignAnnotationsCtx` over `AlignAnnotations` in renderer implementations that
accept a `context.Context` — it degrades gracefully when no terminal size is set (falls
back to inline alignment).

```go
var buf bytes.Buffer
if err := renderMap(ctx, &buf, s, data, true); err != nil {
    return err
}
return render.AlignAnnotationsCtx(ctx, buf.String(), w)
```

### `render.VisualWidth(s string) int`

Returns the visible character width of `s`, stripping ANSI escape codes. Used
internally by `AlignAnnotations` for accurate column measurement.

---

## BaseStyler

### `render.BaseStyler`

A no-op `kongfig.Styler` implementation that returns every token unchanged. Embed it in
custom `Styler` structs to inherit pass-through defaults and override only the methods
you need:

```go
import "github.com/pmarschik/kongfig/render"

type BoldKeyStyler struct{ render.BaseStyler }

func (BoldKeyStyler) Key(s string) string { return "\033[1m" + s + "\033[0m" }
```

---

## Context accessors

These read `RenderOption` values injected by `Kongfig.RenderWith` into the render context.
Renderers should call these instead of reading raw context values directly.

| Accessor                          | Returns               | Description                                        |
| --------------------------------- | --------------------- | -------------------------------------------------- |
| `render.NoComments(ctx)`          | `bool`                | True when `WithRenderNoComments()` is active       |
| `render.NoProvenance(ctx)`        | `bool`                | True under `WithRenderNoProvenance()` or the above |
| `render.NoHelp(ctx)`              | `bool`                | True under `WithRenderNoHelp()` or `NoComments`    |
| `render.HelpTexts(ctx)`           | `map[string]string`   | Per-path help texts (`nil` when `NoHelp`)          |
| `render.HelpText(ctx, path)`      | `string`              | Help text for path; prefix-matched, emitted once   |
| `render.AlignSources(ctx)`        | `bool`                | True (default) unless `WithRenderNoAlignSources()` |
| `render.FileRawPaths(ctx)`        | `bool`                | True when `WithRenderFileRawPaths()` is active     |
| `render.FieldNames(ctx)`          | `PathFieldNames`      | Path → SourceID → field name map                   |
| `render.FieldName(ctx, path)`     | `string`              | Field name for path in the current source          |
| `render.VerboseSources(ctx)`      | `map[string][]string` | Per-path verbose source list                       |
| `render.FilterSourceFromCtx(ctx)` | `[]string`            | Effective filter source list from context          |

#### Help text rendering

`render.HelpText(ctx, path)` should be called by renderers before emitting each key.
It performs **prefix matching**: a help text registered for `"labels"` also fires for
`"labels.key1"` and `"labels[0]"`. Each matched key is emitted **at most once per
render call** — subsequent paths matched by the same key return `""`. Renderers should
emit the result as a comment line above the key:

```go
if help := render.HelpText(ctx, path); help != "" {
    fmt.Fprintf(w, "%s# %s\n", indent, help)
}
```

### TTY size

`render.WithTTYSize(cols, rows int)` injects terminal dimensions as a `RenderOption`.
`AlignAnnotationsCtx` reads this automatically. Renderers that do their own line-wrapping
can also access it directly:

```go
tty, _ := render.TTYSizeKey.Read(ctx)
if tty.Cols > 0 {
    // use tty.Cols for line wrapping
}
```

Pass `(0, 0)` to explicitly clear a previously set size.

---

## Filter helpers

### `render.MatchesFilterSource(source string, filters []string) bool`

Reports whether `source` passes the filter list. An empty list matches everything.
`"no-<prefix>"` entries exclude; positive entries form an allowlist. Prefix matching:
`"env"` matches `"env"`, `"env.tag"`, `"env.kong"`, etc.

```go
filters := render.FilterSourceFromCtx(ctx)
if !render.MatchesFilterSource(src, filters) {
    continue // skip this leaf
}
```

### `render.BuildFilterSource(layers map[string]bool) []string`

Builds a filter list from a `layerName → show` map. Layers with `show=false` become
`"no-<name>"` entries.
