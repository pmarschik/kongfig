# Architecture

## Core model

`Kongfig` is a layered, last-write-wins config container backed by a nested `map[string]any`.

```
Load(defaults)   →  {host: localhost, port: 8080}
Load(file)       →  {port: 9090}
Load(env)        →  {host: prod.example.com}
──────────────────────────────────────────
Raw()            →  {host: prod.example.com, port: 9090}
```

Each `Load` call:

1. Calls `provider.Load()` → `map[string]any`
2. Applies registered transforms (key-by-key `any → any`)
3. Deep-merges into `k.data` (last writer wins per leaf path)
4. Records a `Layer{Name, Source, Data}` snapshot (for `--layers` display)
5. Updates `Provenance` (path → source label)
6. Fires `OnLoad` hooks

### Source labels vs display sources

- **Source label** (`Layer.Name`, stored in `Provenance`): short canonical name used for filtering (`"env"`, `"env.tag"`, `"flags"`, `"file"`, `"defaults"`). Must be stable.
- **Display source** (`Layer.Source`): human-facing string appended to `# ===` layer headers, such as `"config.yaml"` or `"env.prefix MINI_"`. It can be verbose.

`Load` calls `provider.ProviderInfo()` to get `Name` and `Kind`. `WithSource` overrides
`Name`, and `inferKind(name)` then gives the kind.

### Provenance

`Provenance` records `path → source` for every leaf path that was set. It also holds `derived` entries (`path → baseline value`) for `HideDerived` filtering — these record what a value looked like before any overrides so renderers can suppress unchanged defaults.

**"Derived" is a display annotation, not a computation.** There is no expression language. Values in the data map are plain Go values set by providers. A provider calls `SetDerived` to communicate "this is the canonical default for this path". Renderers then use `IsDerived` to decide whether to dim or hide the value.

### Concurrency

`Kongfig` is safe for concurrent use. A `sync.RWMutex` guards all mutations. `Load` takes a write lock only for the final merge and append. Transform application and hooks run outside the lock.

---

## Providers

| Interface          | Purpose                                                                        |
| ------------------ | ------------------------------------------------------------------------------ |
| `Provider`         | Returns `map[string]any` on demand                                             |
| `ByteProvider`     | Also exposes raw bytes (for format-native display)                             |
| `WatchProvider`    | Calls a callback on live changes                                               |
| `OutputProvider`   | Can produce its own `Renderer` via `Bind(Styler)`                              |
| `KeyOrderProvider` | Returns key insertion order from the last `Load` call for `--layers` rendering |

### Source label conventions

| Label prefix | Meaning                                |
| ------------ | -------------------------------------- |
| `defaults`   | Struct/file defaults — lowest priority |
| `file`       | Config file                            |
| `xdg`        | XDG config file                        |
| `workdir`    | Workdir config file                    |
| `env`        | Environment variables (grouped)        |
| `env.tag`    | Struct tag env vars (`env:""`)         |
| `env.kong`   | Kong flag resolver env vars            |
| `env.prefix` | Prefix-stripped env vars               |
| `flags`      | CLI flags                              |
| `derived`    | Computed/inherited values              |

Env sub-sources share the `env` prefix so they collapse in merged views. Collision warnings fire when two `env.*` providers set the same path.

---

## Renderers

A `Renderer` writes `map[string]any` + `Provenance` to an `io.Writer`.

### Styler

All renderers accept a `Styler` for terminal coloring. `Styler` has three tiers of methods:

**Leaf value styling** — called directly by renderers on each formatted token:

```go
Key(s string) string        // config key name
String(s string) string     // string value
Number(s string) string     // numeric value
Bool(s string) string       // boolean value
BraceOpen(s string) string  // opening brace/bracket
BraceClose(s string) string // closing brace/bracket
Comment(s string) string    // comment token (# or //)
Redacted(s string) string   // redacted placeholder
```

**Structured source annotation** — use this for new providers. Each part is styled independently:

```go
SourceKind(s string) string  // the kind token ("file", "env", "flags")
SourceData(s string) string  // generic data in an annotation (e.g. a file path)
SourceKey(s string) string   // source-specific identifier ("$APP_HOST", "--log-level")
```

**Legacy annotation** — used when no `LayerMeta` is available:

```go
Annotation(source, s string) string  // full annotation string, styled by source name
```

Two implementations ship: `style/plain` (no-op, all methods return input unchanged) and
`style/charming` (lipgloss, resolved once at construction).

When you add a new `Styler` method, update `style/plain`, `style/charming`, and
`mockStyler` in `interfaces_test.go`.

### LayerMeta and SourceMeta

All providers must implement `ProviderInfo() ProviderInfo`, which is part of the `Provider`
interface. The method returns a `ProviderInfo{Name, Kind}` struct. Kongfig stamps this
struct into `LayerMeta`, together with `ID` (monotonic sequence), `Timestamp` (wall clock at
load time), and `Format`. `Format` is the parser format name, such as `"yaml"`. It comes
from the `ParserNamer.Format()` method of the parser, when the parser implements it.

When `WithSource(name)` overrides the name at load time, `inferKind` reads `Kind` from the
override. It uses a prefix convention, for example `"env.*"` → `KindEnv`.

Some providers carry rich annotation data, for example a file path or an env var name.
These providers also implement the optional `ProviderDataSupport` interface:

```go
type ProviderDataSupport interface {
    ProviderData() ProviderData
}
```

`load.go` checks for this interface and stores the result in `LayerMeta.Data`. `kong/show`
collects all metas via `withLayerMetas` and passes them through `RenderOptions` so
renderers can delegate annotation rendering.

```go
func (p *Provider) ProviderData() kongfig.ProviderData {
    return SourceData{Path: p.path}
}
```

`LayerMeta.RenderAnnotation` renders `"kind (data)"`, where `ProviderData.RenderAnnotation`
produces the data. If the data is empty, it renders only `"kind"`, with no parentheses.

**`ProviderData.RenderAnnotation(ctx context.Context, s Styler, path string) string`**
contract:

- `path` is the config dot-path of the annotated value. Pass `""` at layer-header
  level (no specific path).
- Must return a single line (no newlines). Empty string means "no data" (no parens in the
  surrounding `LayerMeta.RenderAnnotation` output).
- The implementation owns its own styling: call `s.SourceData(...)` for paths/generic
  data, `s.SourceKey(...)` for source-specific identifiers like `$VAR_NAME`.
- `ctx` carries the `SourceID` (via `SourceIDFromCtx`) so the implementation can find
  field names in the `PathFieldNames` injected by `Kongfig.RenderWith`.

### Source positions

`ProviderData` implementations backed by a text document can also implement the optional
`PositionSupport` extension:

```go
type PositionSupport interface {
    PositionOf(path string) *SourcePosition   // nil when unknown
}
```

`LayerMeta.PositionOf(path)` delegates to it and returns `nil` for layers with no document
(env, flags, defaults) or no recorded position. `SourcePosition{File, Line, Col}` prints as
`config.yaml:42:8`. It drops unknown trailing components.

The positions themselves come from the parser. During `Load`, `file.Provider` prefers a
`DocumentParser` over a `KeyOrderParser`. It stamps the file path onto each position, then
gives the positions to `SourceData`. See
[implementing-parsers.md](implementing-parsers.md#source-positions-documentparser). YAML
supports this. TOML does not, because `BurntSushi/toml` keeps key positions unexported.

Validation reads them through `PathSource.Position()` — see
[validation.md](validation.md#source-positions).

### render.Value — mandatory helper for new renderers

`RenderedValue` is a wrapper placed over every leaf by `prepareRender`. Renderers must not call `s.String(...)` or `s.Number(...)` directly on leaf values — they must call:

```go
render.Value(s, v, formattedString)
```

This handles redaction, codec styling, and type dispatch centrally. `formattedString` is the renderer's own serialization of `v`, for example TOML-quoted, JSON-encoded, or `%v`. Kongfig ignores it when the value is redacted.

**Why the sentinel over pre-styling?** Styling must happen after format decisions, because TOML quoting, YAML quoting and env quoting all differ. If a styled string goes into the data map, it entangles data with presentation. The sentinel keeps data clean and delegates all styling to the render path.

### render.Annotation

Call `render.Annotation(ctx, rv, path, s)` for every source annotation.
It delegates to `LayerMeta.RenderAnnotation` (via `rv.Source.Layer`) which handles
structured annotation styling (file paths, env var names) for providers that implement
`ProviderDataSupport`. Returns `""` when the value has no source or when
`render.NoProvenance(ctx)` is true (`WithRenderNoProvenance()` or `WithRenderNoComments()`).

---

## Package layout

```
kongfig/                  core: Kongfig, interfaces, provenance, render helpers
  parsers/
    yaml/                 YAML parser + renderer
    toml/                 TOML parser + renderer
    json/                 JSON/JSONC parser + renderer
  providers/
    env/                  Env var providers
    structs/              Struct-reflection provider (Defaults, TagEnv)
    file/                 File provider with auto-format discovery
  style/
    plain/                No-op Styler
    charming/             lipgloss-backed Styler
  kong/
    show/                 Reusable --format/--layers/--redacted/--sources flags + Render helpers
    provider/             Load kong defaults, env vars, and parsed flags as kongfig layers
    resolver/             kong.Resolver — seeds flag defaults from kongfig
    charming/             Wire charming styler + resolver in one call
```

### Internal representation

The internal store is `map[string]any`. Kongfig does not normalize types at load time. Each provider returns its native types: strings from env, int64 from TOML, float64 from JSON. `Get[T]()` does the coercion with `mapstructure` and `WeaklyTypedInput: true`. This keeps the load path simple and format-agnostic. It also makes the `RedactedValue` sentinel pattern possible, because kongfig can store an opaque value next to plain values without a typed representation.

### Adding a new renderer

1. Accept `kongfig.Styler`.
2. For leaf values call `render.Value(s, v, yourFormattedString)` — never `s.String(...)`.
3. For source annotations call `render.Annotation(ctx, rv, path, s)` — never format inline.
4. If the renderer is tied to a parser, implement `OutputProvider.Bind(Styler) Renderer`.

### Adding a new provider

1. Implement `Provider`. It requires `Load` and `ProviderInfo`. Optionally implement
   `ByteProvider` for raw bytes access, and `WatchProvider` for file watching.
2. Return a stable source label from `ProviderInfo().Name`. For any env-variable provider,
   use the `env.*` prefix. Collision detection and `--layers` grouping then work correctly.
3. Optionally implement `ProviderDataSupport` (`ProviderData() ProviderData`) for rich
   annotation rendering (shows `kind (detail)` instead of `kind`):
   - Env-sourced: return `envprovider.ProviderData{}`
   - File-sourced: return `file.SourceData{Path: p.path, DisplayPath: p.displayPath}`
   - Other: define your own type that implements `ProviderData`, and return it
4. Optionally implement `ProviderFieldNamesSupport` (`FieldNames() map[string]string`). It
   registers per-path field names: env var names like `"$APP_HOST"`, flag names like
   `"--host"`. Renderers can then include these names in source annotations. Env and flags
   providers implement this interface. File providers usually do not.
5. If this provider loads format-specific data, such as a YAML or TOML file, optionally
   implement `ParserProvider` (`Parser() kongfig.Parser`). `--layers` mode needs it to use
   the correct native-format renderer for this layer instead of the fallback.
6. Optionally implement `KeyOrderProvider` (`KeyOrder() map[string][]string`). It returns
   the key insertion order captured during `Load`. `Kongfig.Load` stores this order in
   `Layer.KeyOrder`. `RenderLayers` then reproduces the original document order for that
   layer, and the merged view reads in that order too. See
   [Key order](render-pipeline.md#key-order) for the merge of the layers, and for the
   relation between document order and struct field order. Without `KeyOrderProvider`, a
   render uses struct field order (from `NewFor[T]`) or alphabetical order. The built-in
   `file.Provider` delegates to the `KeyOrderParser` interface of the parser.
7. Register via `k.Load(provider)`.

### RedactedPaths and NewFor

`kongfig.RedactedPaths[T]()` (root package) reflects on the `kongfig` struct tags of `T`,
and returns the set of dot-paths marked `"redacted"`. Use `kongfig.NewFor[T]()` to create a
`Kongfig` with redacted paths auto-applied — the common case requires no explicit call.

`structs.RedactedPaths[T]()` is deprecated and delegates to the root package function.
