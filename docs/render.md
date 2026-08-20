<!-- read_when: implementing a renderer, adding render options, or reading option values from context -->

# render — Renderer Utilities

The `render` sub-package (`github.com/pmarschik/kongfig/render`) provides shared
utilities that **all renderer implementations** must import. It is the canonical home
for:

- Value styling helpers (`Value`, `Annotation`)
- Annotation alignment (`AlignAnnotations`, `AnnMarker`)
- A no-op `BaseStyler` embed
- Context accessors for render options
- Filter helpers (`MatchesFilterSource`, `BuildFilterSource`)

A sub-package that implements `kongfig.Renderer` must import `render`, and must not
implement these helpers again.

---

## Value styling

### `render.Value(s Styler, v any, formatted string) string`

Renders a leaf value with the correct `Styler` method. Always call this function on a leaf
value, and never `s.String(formatted)`:

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

Always use this function, and never format an annotation inline:

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

A second sentinel byte (`\x01`) for a line whose annotation must stay on it. The line above
is not a place where a comment can go. The line that closes a folded TOML
multi-line string is one example. A comment above that line lands inside the string. The
reader then parses back a value other than the rendered one.

A fixed annotation aligns with the others when the alignment column has room for it, and
follows its content directly when it does not. Kongfig never lifts it to a line of its own,
and it never pulls the alignment column. Only a movable annotation decides where that column
lands.

Use it only where the above-line fallback changes the meaning of the output. A plain
`AnnMarker` is the right choice everywhere else.

### `render.AlignAnnotations(raw string, w io.Writer) error`

Post-processes every line that contains `AnnMarker`, and pads each value segment to the
same column before its annotation. A line without the marker keeps its exact form.

```go
var buf bytes.Buffer
if err := renderMap(ctx, &buf, s, data, true); err != nil {
    return err
}
return render.AlignAnnotations(buf.String(), w)
```

### `render.AlignAnnotationsCtx(ctx context.Context, raw string, w io.Writer) error`

Like `AlignAnnotations` but reads the terminal width from `ctx` (set via `render.WithTTYSize`
or `render.TTYSizeKey`). When a line cannot fit its annotation inline, the annotation goes
on a comment line **above** the value line, with matching indentation:

```
# from file.yaml
host: localhost
# from environment
port: 8080
```

The decision is per line, not per document. Kongfig picks the alignment column that keeps
the most annotations inline. One unusually wide line therefore moves its annotation alone,
and every other annotation in the document keeps its place beside its value:

```
host = "localhost"                  # from file.yaml
port = 8080                         # from environment
# defaults
endpoint = "https://…very long…"
```

Prefer `AlignAnnotationsCtx` over `AlignAnnotations` in a renderer that accepts a
`context.Context`. With no terminal size set, it uses inline alignment instead.

```go
var buf bytes.Buffer
if err := renderMap(ctx, &buf, s, data, true); err != nil {
    return err
}
return render.AlignAnnotationsCtx(ctx, buf.String(), w)
```

### `render.VisualWidth(s string) int`

Returns the visible character width of `s`, and it strips the ANSI escape codes.
`AlignAnnotations` uses this function internally for an accurate column measurement.

---

## BaseStyler

### `render.BaseStyler`

A no-op `kongfig.Styler` implementation that returns every token unchanged. Embed it in a
custom `Styler` struct. Your struct then inherits the pass-through defaults, and it
overrides only the methods that you need:

```go
import "github.com/pmarschik/kongfig/render"

type BoldKeyStyler struct{ render.BaseStyler }

func (BoldKeyStyler) Key(s string) string { return "\033[1m" + s + "\033[0m" }
```

---

## Context accessors

These functions read the `RenderOption` values that `Kongfig.RenderWith` injects into the
render context. A renderer must call these functions, and must not read a raw context value
directly.

| Accessor                          | Returns               | Description                                           |
| --------------------------------- | --------------------- | ----------------------------------------------------- |
| `render.NoComments(ctx)`          | `bool`                | True when `WithRenderNoComments()` is active          |
| `render.NoProvenance(ctx)`        | `bool`                | True under `WithRenderNoProvenance()` or `NoComments` |
| `render.NoHelp(ctx)`              | `bool`                | True under `WithRenderNoHelp()` or `NoComments`       |
| `render.HelpTexts(ctx)`           | `map[string]string`   | Per-path help texts (`nil` when `NoHelp`)             |
| `render.HelpText(ctx, path)`      | `string`              | Help text for the path. Prefix-matched, emitted once  |
| `render.AlignSources(ctx)`        | `bool`                | True (default) unless `WithRenderNoAlignSources()`    |
| `render.FileRawPaths(ctx)`        | `bool`                | True when `WithRenderFileRawPaths()` is active        |
| `render.FieldNames(ctx)`          | `PathFieldNames`      | Path → SourceID → field name map                      |
| `render.FieldName(ctx, path)`     | `string`              | Field name for path in the current source             |
| `render.VerboseSources(ctx)`      | `map[string][]string` | Per-path verbose source list                          |
| `render.FilterSourceFromCtx(ctx)` | `[]string`            | Effective filter source list from context             |

#### Help text rendering

A renderer must call `render.HelpText(ctx, path)` before it emits each key. The function
does **prefix matching**. A help text for `"labels"` also fires for `"labels.key1"` and
`"labels[0]"`. Kongfig emits each matched key **at most once per render call**, and a later
path under the same key returns `""`. A renderer must emit the result as a comment line
above the key:

```go
if help := render.HelpText(ctx, path); help != "" {
    fmt.Fprintf(w, "%s# %s\n", indent, help)
}
```

### TTY size

`render.WithTTYSize(cols, rows int)` injects terminal dimensions as a `RenderOption`.
`AlignAnnotationsCtx` reads this automatically. A renderer that does its own line-wrapping
can also read it directly:

```go
tty, _ := render.TTYSizeKey.Read(ctx)
if tty.Cols > 0 {
    // use tty.Cols for line wrapping
}
```

Pass `(0, 0)` to clear a size that was set before.

---

## Filter helpers

### `render.MatchesFilterSource(source string, filters []string) bool`

Reports whether `source` passes the filter list. An empty list matches everything.
A `"no-<prefix>"` entry excludes, and a positive entry forms an allowlist. The match is a
prefix match, so `"env"` matches `"env"`, `"env.tag"` and `"env.kong"`.

```go
filters := render.FilterSourceFromCtx(ctx)
if !render.MatchesFilterSource(src, filters) {
    continue // skip this leaf
}
```

### `render.BuildFilterSource(layers map[string]bool) []string`

Builds a filter list from a `layerName → show` map. A layer with `show=false` becomes a
`"no-<name>"` entry.
