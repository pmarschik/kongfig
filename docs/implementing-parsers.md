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

Implement these interfaces also. A parser without them loses the features in the Purpose column:

| Interface                | Method                                                                   | Purpose                                                                  |
| ------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `kongfig.KeyOrderParser` | `UnmarshalWithKeyOrder([]byte) (ConfigData, map[string][]string, error)` | Preserve document key order for rendering via `file.Provider.KeyOrder()` |
| `kongfig.DocumentParser` | `UnmarshalDocument([]byte) (ConfigData, DocumentMeta, error)`            | Key order **and** per-path source positions from a single parse          |
| `kongfig.CtxMarshaler`   | `MarshalCtx(context.Context, ConfigData) ([]byte, error)`                | Marshal that honors the render options in ctx, key order above all       |
| `kongfig.DocumentEditor` | `EditDocument([]byte, ConfigData) ([]byte, error)`                       | Rewrite a document in place, keeping comments, key order and layout      |

Without `KeyOrderParser`, a render uses struct field order (from `NewFor[T]`) or
alphabetical order. If the text encoding of your format has a meaningful key order,
implement the interface. The order feeds `--layers` and the merged view, where kongfig
merges the orders of the layers document-first
([Key order](render-pipeline.md#key-order)).

A parser that reports key order must also implement `CtxMarshaler`. Without it, the parser
reads an order that it cannot write again. Make `Marshal` the thin wrapper —
`MarshalCtx(context.Background(), data)` — so there is one emitter and the two paths cannot
drift. `MarshalCtx` must stay correct with an empty context, because a plain write gives it
an empty context. See [CtxMarshaler](render-pipeline.md#ctxmarshaler).

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
once. `file.Provider` prefers `DocumentParser`. If the parser does not have it,
`file.Provider` uses `KeyOrderParser`, and then plain `Unmarshal`. Keep
`UnmarshalWithKeyOrder` for API compatibility and delegate:

```go
func (p Parser) UnmarshalWithKeyOrder(b []byte) (kongfig.ConfigData, map[string][]string, error) {
    data, meta, err := p.UnmarshalDocument(b)
    return data, meta.KeyOrder, err
}
```

Two rules for `Positions`:

- **Record the position of the value, not the position of the key.** A rejected value sits
  there. For a nested table, the value position is the position of its first key.
- **Leave `SourcePosition.File` empty.** A parser sees only bytes. After the parse,
  `file.Provider` stamps the canonical file path onto every position.

Only a format whose decoder exposes per-key positions can implement this. `gopkg.in/yaml.v3`
exposes them (`yaml.Node.Line` and `.Column`). `github.com/BurntSushi/toml` does not,
because it keeps key positions unexported and shows them only inside a `ParseError`.

A consumer reads the positions through the layer, never through the parser:

```go
if src, ok := kf.SourceFor("db.port"); ok {
    if pos := src.Layer.PositionOf("db.port"); pos != nil {
        fmt.Println(pos) // /etc/app/config.yaml:3:9
    }
}
```

### In-place editing (`DocumentEditor`)

`Marshal` writes a document. `EditDocument` changes one. A program that maintains the
config file of a user needs the second method. The comments, the key order, the indentation
and the quoting that the user wrote must survive a one-value change. Implement
`EditDocument` when the format has a hand-written layout to keep. `parsers/toml`,
`parsers/yaml` and `parsers/json` all do.

```go
func (p Parser) EditDocument(src []byte, want kongfig.ConfigData) ([]byte, error)
```

Five rules:

- **`want` is the whole document, not a patch.** A key that it does not mention is a key
  that the rewrite removes. A caller builds `want` from a parse of the document and an edit
  of the data.
- **Change only the text that the data change touches.** Compare against the parsed
  document (`kongfig.EqualValues` compares values, not the Go types that spell them). Do
  not touch anything that did not change, because a rewrite of an unchanged value
  reformats it.
- **Match the elements of a list by identity, not by position.** Element _i_ of the edited
  list is not element _i_ of the document once an element goes away or joins in the middle.
  Pair by position and every element below the edit is rewritten, which hands each trailing
  comment to its neighbour's value. `internal/editalign` reads the two lists and returns the
  rewrites, deletions and insertions that turn one into the other; fall back to pairing by
  position only when the parsed list and the written one disagree about how many elements
  there are. Count the comment lines directly above an element as part of that element's
  text: they go away with it, and a new element goes in above them.
- **Refuse what the format cannot express as an edit of _this_ document.** Do not guess.
  Three examples exist. The first is a new TOML key whose value needs a section that the
  file does not have. The second is a YAML value across several lines. The third is a
  mapping that merges another mapping. Return an error, write nothing, and let the caller
  use `Marshal` instead. A format that can write every value refuses little: the JSON
  editor refuses a document with no object at its root, and a value that JSON has no text
  for.
- **Collect the edits, then splice them in one pass.** Give the byte ranges to
  `internal/editsplice.Apply`. It sorts them, sizes one buffer for the whole set, and
  copies the document once. Every offset stays correct against the document that you read
  it from, because nothing moves until the end. A refusal halfway through therefore leaves
  the document unchanged. Two edits over the same bytes are a bug in the editor, and
  `Apply` reports them instead of writing a broken document.

A caller reaches the editor through `kongfig.EditDocument`. That function parses the result
and compares it against `want` before it returns the bytes. It returns
`ErrEditNotVerified` when the result does not match, and `ErrNoDocumentEditor` when the
parser has no editor. Do not skip that wrapper in your own tests. Text surgery is only as
safe as its verification.

A caller that loaded the file into a `Kongfig` reaches the editor through
`Kongfig.EditLayer(source, src, mutate)`. That function builds `want` from the data of one
layer, not from the merge of all layers. The parser sees the same call either way, and
neither entry point reads or writes a file.

Both built-in editors work from positions, not from a re-render. `parsers/yaml` reads the
positions from `yaml.Node`. `parsers/toml` has its own scanner (`parsers/toml/scan.go`),
because `github.com/BurntSushi/toml` does not report them. Every such scan must be
string-literal aware. A `#` or an `=` inside a quoted value is text, not syntax, and an
editor that reads it as syntax rewrites the wrong bytes.

## Renderer checklist

The `Renderer` that `Bind` returns must handle all of the items below. If one is missing, users lose a feature and get no message.

### 1. `render.Value` — never call `s.String()` directly on leaf values

```go
// ✗ Wrong: misses redaction and codec styling
line := s.String(formatted)

// ✓ Correct
line := render.Value(s, v, formatted)
```

`render.Value` handles `RenderedValue` centrally, and it applies the correct `Styler` method for the value type.

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

A renderer that reaches its comments through `render.Annotation`,
`render.HelpText` and `render.HelpTexts` also gets the finer gates for free.
`WithRenderNoProvenance()` empties the first helper. `WithRenderNoHelp()` empties
the other two. `WithRenderNoComments()` empties all three. `render.NoComments(ctx)`
alone still answers one question correctly: can _any_ comment appear? Never emit a
comment that a helper refused to produce.

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

Alignment is **on by default**. To disable it, use `WithRenderNoAlignSources()`.
`render.AlignSources(ctx)` returns `true` until that option applies. The pattern:

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

`AlignAnnotationsCtx` can move a marked annotation to a comment line above its value, when
the terminal leaves no room for it inline. Sometimes that move changes the meaning of the
document. Above the line that closes a folded multi-line string, the comment lands inside
the string. In that place, use `render.AnnMarkerFixed` instead. It aligns when the column
has room, and it follows its content when the column has no room. Kongfig never lifts it.

## Testing checklist

The test file of each parser must cover:

- [ ] Roundtrip: `Marshal` → `Unmarshal` preserves values
- [ ] Empty input handling
- [ ] `Bind` / `Render` basic output
- [ ] `RenderedValue` unwrapping
- [ ] `RedactedValue` display
- [ ] `render.NoComments` suppresses annotations
- [ ] `render.HelpTexts` injects help comments (if format supports comments)
- [ ] `render.AlignSources` aligns annotations at the same column (on by default, disabled by `WithRenderNoAlignSources`)
- [ ] Styler dispatch: `Number`, `Bool`, `Null` are called for correct value types
- [ ] Typed slice rendering: `[]SomeStruct` and `[]any{ConfigData{...}}` must produce format-native syntax, not the `%v` output of Go (`[map[key:val] ...]`)

## Format-specific notes

### JSON (`parsers/json`)

- `Comments: true` enables JSONC mode. The parser removes `//` and `/* */` before the parse, and it uses `//` for inline annotations.
- `Compact: true` renders with no indentation.
- Help texts and annotations appear only in JSONC mode (`Comments: true`).
- `EditDocument` reads the document as text (`scan.go`), so every value keeps the byte range that it was written in. In JSONC mode the scan steps over a comment as if it were space, which is why a comment survives an edit.
- JSON writes several members on one line, which TOML and YAML do not. A member that has its line to itself goes away with the line. A member that shares its line goes away with the comma that separates it from the next one.
- The last member of a container has no comma after it. Its removal therefore takes the comma of the member before it as a second edit.

### TOML (`parsers/toml`)

- The renderer writes the scalars before the tables (TOML convention: inline values first, then `[section]` headers).
- Section headers use `s.Syntax("[header]")`.
- Help comments use `# prefix`.
- Slices of any element type render as `[...]` inline arrays. Use `reflect.TypeOf(v).Kind() == reflect.Slice` instead of a `[]any` type switch. Without it, a typed slice like `[]SomeStruct` reaches the `%v` format of Go.
- Honor `render.BlockCollections(ctx)`. When it is true, always write multiline style, whatever the inline length is.

### YAML (`parsers/yaml`)

- Help comments use `# prefix`.
- The renderer supports nested maps through a recursive `renderMap`.
- Slices and maps render as YAML flow syntax (`[{k: v}, ...]`). Use `reflect.TypeOf(v).Kind()` to detect any slice or map. Without it, a typed slice like `[]SomeStruct` reaches the `%v` format of Go.
- A sub-tree whose path is marked inline (`yaml.WithInlineMaps`, `kongfig:",inline"`) collapses into a flow mapping instead of a nested block mapping. These are the same marks that `parsers/toml` reads for inline tables.
- Honor `render.BlockCollections(ctx)`. When it is true, always write block style, whatever the inline length or the TTY width is.
