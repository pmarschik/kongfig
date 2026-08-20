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

| Interface                | Method                                                                   | Purpose                                                                  |
| ------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `kongfig.KeyOrderParser` | `UnmarshalWithKeyOrder([]byte) (ConfigData, map[string][]string, error)` | Preserve document key order for rendering via `file.Provider.KeyOrder()` |
| `kongfig.DocumentParser` | `UnmarshalDocument([]byte) (ConfigData, DocumentMeta, error)`            | Key order **and** per-path source positions from a single parse          |
| `kongfig.CtxMarshaler`   | `MarshalCtx(context.Context, ConfigData) ([]byte, error)`                | Marshal that honours the render options in ctx, key order above all      |
| `kongfig.DocumentEditor` | `EditDocument([]byte, ConfigData) ([]byte, error)`                       | Rewrite a document in place, keeping comments, key order and layout      |

Without `KeyOrderParser`, rendering falls back to struct field order (from `NewFor[T]`) or
alphabetical. Implement it if your format's text encoding has a meaningful key order — the
order feeds both `--layers` and the merged view, where the layers' orders are merged
document-first ([Key order](render-pipeline.md#key-order)).

A parser that reports key order should also implement `CtxMarshaler`, or it can read an order
it cannot write back. Make `Marshal` the thin wrapper — `MarshalCtx(context.Background(), data)` —
so there is one emitter and no way for the two paths to drift; `MarshalCtx` must stay correct
with an empty context, since that is what a plain write gives it. See
[CtxMarshaler](render-pipeline.md#ctxmarshaler).

Add compile-time assertions to catch regressions early:

```go
var (
    _ kongfig.Parser          = Parser{}
    _ kongfig.ParserNamer     = Parser{}
    _ kongfig.OutputProvider  = Parser{}
    _ kongfig.KeyOrderParser  = Parser{}
    _ kongfig.DocumentParser  = Parser{}
    _ kongfig.CtxMarshaler    = Parser{}
    _ kongfig.DocumentEditor  = Parser{}
)
```

### Source positions (`DocumentParser`)

`DocumentParser` supersedes `KeyOrderParser`: `DocumentMeta` carries `KeyOrder` **and**
`Positions` (dot-path → `kongfig.SourcePosition`), so a parser that can report both parses
once. `file.Provider` prefers `DocumentParser` and falls back to `KeyOrderParser`, then to
plain `Unmarshal`. Keep `UnmarshalWithKeyOrder` for API compatibility and delegate:

```go
func (p Parser) UnmarshalWithKeyOrder(b []byte) (kongfig.ConfigData, map[string][]string, error) {
    data, meta, err := p.UnmarshalDocument(b)
    return data, meta.KeyOrder, err
}
```

Two rules for `Positions`:

- **Record the value's position, not the key's.** That is where a rejected value sits; for
  a nested table the value position is its first key.
- **Leave `SourcePosition.File` empty.** A parser only sees bytes; `file.Provider` stamps
  the canonical file path onto every position after parsing.

Only formats whose decoder exposes per-key positions can implement this. `gopkg.in/yaml.v3`
does (`yaml.Node.Line`/`.Column`); `github.com/BurntSushi/toml` does not — it keeps key
positions unexported and surfaces them only inside a `ParseError`.

Consumers read positions back through the layer, never through the parser:

```go
if src, ok := kf.SourceFor("db.port"); ok {
    if pos := src.Layer.PositionOf("db.port"); pos != nil {
        fmt.Println(pos) // /etc/app/config.yaml:3:9
    }
}
```

### In-place editing (`DocumentEditor`)

`Marshal` writes a document; `EditDocument` changes one. A program that maintains a user's
config file needs the second: the comments, key order, indentation and quoting the user
wrote have to survive a one-value change. Implement it when the format has a hand-written
layout worth keeping — `parsers/toml` and `parsers/yaml` do, `parsers/json` does not.

```go
func (p Parser) EditDocument(src []byte, want kongfig.ConfigData) ([]byte, error)
```

Four rules:

- **`want` is the whole document, not a patch.** A key it does not mention is a key the
  rewrite removes. Callers get `want` by parsing the document and editing the data.
- **Change only the text the data change touches.** Compare against the parsed document
  (`kongfig.EqualValues` compares values, not the Go types that spell them) and leave
  everything that did not change alone — rewriting an unchanged value would reformat it.
- **Refuse what the format cannot express as an edit of _this_ document**, rather than
  guessing: a new TOML key whose value needs a section the file does not have, a YAML value
  spread over several lines, a mapping that merges another one. Return an error, write
  nothing, and let the caller fall back to `Marshal`.
- **Collect the edits, then splice.** Applying byte ranges from the back keeps every offset
  measured against the document it was read from, and a refusal halfway through leaves the
  document untouched.

Callers reach it through `kongfig.EditDocument`, which parses the result and compares it
against `want` before handing the bytes over (`ErrEditNotVerified` when it does not match,
`ErrNoDocumentEditor` when the parser has no editor). Do not skip that wrapper in your own
tests: text surgery is only as safe as its verification.

Both built-in editors work from positions rather than from a re-render — `parsers/yaml`
reads them off `yaml.Node`, `parsers/toml` has its own scanner (`parsers/toml/scan.go`)
because `github.com/BurntSushi/toml` does not report them. Whichever it is, the scan must be
string-literal aware: a `#` or `=` inside a quoted value is text, not syntax, and an editor
that reads it as syntax rewrites the wrong bytes.

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
