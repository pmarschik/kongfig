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
- **Display source** (`Layer.Source`): human-facing string appended to `# ===` layer headers (e.g. `"config.yaml"`, `"env.prefix MINI_"`). May be verbose.

`Load` calls `provider.ProviderInfo()` to get `Name` and `Kind`; `WithSource` overrides `Name` (kind falls back to `inferKind(name)`).

### Provenance

`Provenance` records `path → source` for every leaf path that was set. It also holds `derived` entries (`path → baseline value`) for `HideDerived` filtering — these record what a value looked like before any overrides so renderers can suppress unchanged defaults.

**"Derived" is a display annotation, not a computation.** There is no expression language. Values in the data map are plain Go values set by providers. `SetDerived` is called by providers that want to communicate "this is the canonical default for this path" — renderers use `IsDerived` to decide whether to dim/hide the value.

### Concurrency

`Kongfig` is safe for concurrent use. A `sync.RWMutex` guards all mutations. `Load` takes a write lock only for the final merge+append; transform application and hooks run outside the lock.

---

## Providers

| Interface        | Purpose                                            |
| ---------------- | -------------------------------------------------- |
| `Provider`       | Returns `map[string]any` on demand                 |
| `ByteProvider`   | Also exposes raw bytes (for format-native display) |
| `WatchProvider`  | Calls a callback on live changes                   |
| `OutputProvider` | Can produce its own `Renderer` via `Bind(Styler)`  |

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

All renderers accept a `Styler` for terminal coloring. `Styler` methods fall into three tiers:

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

**Structured source annotation** — use for new providers; each part styled independently:

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

When adding a new `Styler` method, update `style/plain`, `style/charming`, and
`mockStyler` in `interfaces_test.go`.

### LayerMeta and SourceMeta

All providers must implement `ProviderInfo() ProviderInfo` (part of the `Provider` interface),
returning a `ProviderInfo{Name, Kind}` struct. Kongfig stamps this into `LayerMeta` along
with `ID` (monotonic sequence), `Timestamp` (wall clock at load time), and `Format` (parser
format name, e.g. `"yaml"`, from the parser's `ParserNamer.Format()` if implemented).

When `WithSource(name)` overrides the name at load time, `Kind` is inferred from the
override via `inferKind` (prefix convention: `"env.*"` → `KindEnv`, etc.).

Providers that carry rich annotation data (file path, env var name, etc.) also implement
the optional `ProviderDataSupport` interface:

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

`LayerMeta.RenderAnnotation` renders `"kind (data)"` where data is produced by
`ProviderData.RenderAnnotation`. If data is empty, renders just `"kind"` (no parens).

**`ProviderData.RenderAnnotation(s Styler, path string, opts RenderOptions) string`**
contract:

- `path` is the config dot-path of the value being annotated. Pass `""` at layer-header
  level (no specific path).
- Must return a single line (no newlines). Empty string means "no data" (no parens).
- The implementation owns its own styling: call `s.SourceData(...)` for paths/generic
  data, `s.SourceKey(...)` for source-specific identifiers like `$VAR_NAME`.

### RenderValue — mandatory helper for new renderers

`RedactedValue{Display string}` is a sentinel type placed into the data map by `ApplyRedaction`. Renderers must not call `s.Value(...)` directly on leaf values — they must call:

```go
kongfig.RenderValue(s, v, formattedString)
```

This handles the `RedactedValue` case centrally. `formattedString` is the renderer's own serialization of `v` (TOML-quoted, JSON-encoded, `%v`, etc.); it is ignored when `v` is a `RedactedValue`.

**Why the sentinel over pre-styling?** Styling must happen after format decisions (TOML quoting differs from YAML quoting differs from env quoting). Injecting a styled string into the data map would entangle data with presentation. The sentinel keeps data clean and delegates all styling to the render path.

### RenderSourceAnnotation

Call `kongfig.RenderSourceAnnotation(src, path, s, opts)` for every source annotation.
It delegates to `LayerMeta.RenderAnnotation` if a meta is registered for `src` in
`opts` (via `LayerMetasKey`); otherwise falls back to `FormatSourceAnnotation` +
`s.Annotation`. This means structured annotation styling (file paths, env var names)
is handled automatically for providers that implement `ProviderDataSupport`.

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

The internal store is `map[string]any`. Types are not normalised at load time — each provider returns its native types (strings from env, int64 from TOML, float64 from JSON). Coercion is deferred to `Get[T]()`, which uses `mapstructure` with `WeaklyTypedInput: true`. This keeps the load path simple and format-agnostic, and makes the `RedactedValue` sentinel pattern possible (opaque values can be stored alongside plain values without a typed representation).

### Adding a new renderer

1. Accept `kongfig.Styler`.
2. For leaf values call `kongfig.RenderValue(s, v, yourFormattedString)` — never `s.Value(...)`.
3. For source annotations call `kongfig.RenderSourceAnnotation(src, path, s, opts)` — never format inline.
4. Implement `OutputProvider.Bind(Styler) Renderer` if the renderer is tied to a parser.

### Adding a new provider

1. Implement `Provider` (and optionally `ByteProvider`, `WatchProvider`).
2. Return a stable source label from `ProviderName() string` — use the `env.*` prefix for any env-variable provider so collision detection and grouping work correctly.
3. Optionally implement `ProviderDataSupport` (`ProviderData() ProviderData`) for rich
   annotation rendering:
   - Env-sourced: return `envprovider.ProviderData{}`
   - File-sourced: return `file.SourceData{Path: p.path}`
   - Other: define your own type implementing `ProviderData` and return it
4. Register via `k.Load(provider)`.

### RedactedPaths and NewFor

`kongfig.RedactedPaths[T]()` (root package) reflects on `T`'s `kongfig` struct tags and
returns the set of dot-paths marked `"redacted"`. Use `kongfig.NewFor[T]()` to create a
`Kongfig` with redacted paths auto-applied — the common case requires no explicit call.

`structs.RedactedPaths[T]()` is deprecated and delegates to the root package function.
