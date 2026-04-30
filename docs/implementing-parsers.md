<!-- read_when: implementing a new parser or adding Bind/Renderer support -->

# Implementing Parsers

This document is the canonical checklist for any new `kongfig.Parser` implementation. The built-in parsers (`parsers/json`, `parsers/toml`, `parsers/yaml`) serve as reference implementations.

## Required interfaces

Every parser **must** implement:

| Interface                | Methods                    | Purpose                                       |
| ------------------------ | -------------------------- | --------------------------------------------- |
| `kongfig.Parser`         | `Unmarshal`, `Marshal`     | Roundtrip bytes ↔ `ConfigData`                |
| `kongfig.ParserNamer`    | `Format()`, `Extensions()` | Format name and file extension matching       |
| `kongfig.OutputProvider` | `Bind(Styler) Renderer`    | Styled output for `--layers` and `RenderWith` |

Every parser **should** also implement:

| Interface                 | Method                                                                      | Purpose                                                                                  |
| ------------------------- | --------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `kongfig.KeyOrderParser`  | `UnmarshalWithKeyOrder([]byte) (ConfigData, map[string][]string, error)`    | Preserve document key order for `--layers` rendering via `file.Provider.KeyOrder()`     |

Without `KeyOrderParser`, layers mode falls back to struct field order (from `NewFor[T]`) or
alphabetical. Implement it if your format's text encoding has a meaningful key order.

Add compile-time assertions to catch regressions early:

```go
var (
    _ kongfig.Parser          = Parser{}
    _ kongfig.ParserNamer     = Parser{}
    _ kongfig.OutputProvider  = Parser{}
    _ kongfig.KeyOrderParser  = Parser{}
)
```

## Renderer checklist

A `Renderer` returned by `Bind` must handle all of the following. Forgetting any of them means users miss features silently.

### 1. `render.Value` — never call `s.String()` directly on leaf values

```go
// ✗ Wrong: misses redaction and codec styling
line := s.String(formatted)

// ✓ Correct
line := render.Value(s, v, formatted)
```

`render.Value` handles `RenderedValue` centrally and applies the correct `Styler` method for the value type.

### 2. `render.Annotation` — never format source inline

```go
// ✗ Wrong: misses LayerMeta structured rendering
line += "  # " + rv.Source.Layer.Name

// ✓ Correct
if ann := render.Annotation(ctx, rv, path, s); ann != "" {
    line += "  " + s.Comment("# ") + ann
}
```

### 3. `render.NoComments` — gate all comment/annotation output

```go
noComments := render.NoComments(ctx)

if !noComments && isRV {
    // emit annotation
}
if !noComments && helpTexts != nil {
    // emit help comment
}
```

### 4. `render.HelpTexts` — emit per-path help above each key

```go
helpTexts := render.HelpTexts(ctx)

if !noComments && helpTexts != nil {
    if help, ok := helpTexts[path]; ok {
        fmt.Fprintf(w, "%s\n", s.Comment("# "+help)) // TOML/YAML
        // or: s.Comment("// " + help) for JSON
    }
}
```

### 5. `render.AlignSources` — two-pass column alignment

Alignment is **on by default**; users opt out with `WithRenderNoAlignSources()`.
`render.AlignSources(ctx)` returns `true` unless that option was applied. The pattern:

```go
func (r *renderer) Render(ctx context.Context, w io.Writer, data kongfig.ConfigData) error {
    if !render.AlignSources(ctx) {
        return renderMap(ctx, w, r.s, data, ..., false)
    }
    var buf bytes.Buffer
    if err := renderMap(ctx, &buf, r.s, data, ..., true); err != nil {
        return err
    }
    return render.AlignAnnotations(buf.String(), w)
}
```

In the inner `renderMap`, insert `render.AnnMarker` before aligned annotations:

```go
if align {
    line += render.AnnMarker + "  " + s.Comment("# ") + ann
} else {
    line += "  " + s.Comment("# ") + ann
}
```

## Testing checklist

Each parser's test file should cover:

- [ ] Roundtrip: `Marshal` → `Unmarshal` preserves values
- [ ] Empty input handling
- [ ] `Bind` / `Render` basic output
- [ ] `RenderedValue` unwrapping
- [ ] `RedactedValue` display
- [ ] `render.NoComments` suppresses annotations
- [ ] `render.HelpTexts` injects help comments (if format supports comments)
- [ ] `render.AlignSources` aligns annotations at the same column (default on; opt out via `WithRenderNoAlignSources`)
- [ ] Styler dispatch: `Number`, `Bool`, `Null` are called for correct value types
- [ ] Typed slice rendering: `[]SomeStruct` and `[]any{ConfigData{...}}` must produce format-native syntax, not Go's `%v` output (`[map[key:val] ...]`)

## Format-specific notes

### JSON (`parsers/json`)

- `Comments: true` enables JSONC mode: `//` and `/* */` stripped before parsing; `//` used for inline annotations.
- `Compact: true` renders without indentation.
- Help texts and annotations only appear in JSONC mode (`Comments: true`).

### TOML (`parsers/toml`)

- Scalars are rendered before tables (TOML convention: inline values first, then `[section]` headers).
- Section headers use `s.Syntax("[header]")`.
- Help comments use `# prefix`.
- Slices of any element type render as `[...]` inline arrays. Use `reflect.TypeOf(v).Kind() == reflect.Slice` rather than a `[]any` type switch; typed slices like `[]SomeStruct` would otherwise fall through to Go's `%v` format.
- Honour `render.BlockCollections(ctx)`: when true, always emit multiline style regardless of inline length.

### YAML (`parsers/yaml`)

- Help comments use `# prefix`.
- Supports nested maps via recursive `renderMap`.
- Slices and maps render as YAML flow syntax (`[{k: v}, ...]`). Use `reflect.TypeOf(v).Kind()` to detect any slice/map — typed slices like `[]SomeStruct` would otherwise fall through to Go's `%v` format.
- Honour `render.BlockCollections(ctx)`: when true, always emit block style regardless of inline length or TTY width.
