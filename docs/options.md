# Options Reference

There are four distinct option types in kongfig, each scoped to a different call site.

---

## 1. Kongfig options — `New(opts ...Option)`

Configure the `Kongfig` instance itself. Applied once at construction.

| Type                        | Effect                                                                                                                      |
| --------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `WithLogger{Logger}`        | Set the `slog.Logger` for internal warnings. Defaults to `slog.Default()`.                                                  |
| `WithRedacted{Paths}`       | Register dot-paths whose values should be hidden in rendered output. Typically populated from `structs.RedactedPaths[T]()`. |
| `WithRedactionFunc{Fn}`     | Custom `func(path, value string) string` for redaction display. Default: `"<redacted>"`.                                    |
| `WithRedactionString(s)`    | Convenience wrapper for `WithRedactionFunc` — uses a fixed string.                                                          |
| `WithHideAnnotationNames{}` | Suppress both `$VAR_NAME` and `--flag-name` suffixes from all source annotations.                                           |
| `WithHideEnvVarNames{}`     | Suppress only `$VAR_NAME` suffixes from env source annotations.                                                             |
| `WithHideFlagNames{}`       | Suppress only `--flag-name` suffixes from flags source annotations.                                                         |

These are accumulated into a `renderConfig` inside `Kongfig` and applied automatically when `Kongfig.Render` / `Kongfig.RenderWith` is called.

---

## 2. Load options — `k.Load(provider, opts ...LoadOption)`

Configure a single `Load` call. Applied per-load.

| Type                                         | Effect                                                                                                                                                                                                  |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `WithSource{Name}`                           | Override the source label inferred from `provider.Source()`. Use when you load the same provider type multiple times and need distinct labels (e.g. two file providers: `"file.base"`, `"file.local"`). |
| `WithSilenceCollisions{}`                    | Suppress all env collision warnings for this load. Pass no keys to silence all.                                                                                                                         |
| `WithSilenceCollisions{Keys: []string{...}}` | Suppress collision warnings only for specific dot-paths.                                                                                                                                                |

---

## 3. Get options — `Get[T](k, opts ...GetOption)`

Configure a single `Get` or `GetWithProvenance` call.

| Type             | Effect                                                                                                              |
| ---------------- | ------------------------------------------------------------------------------------------------------------------- |
| `Strict()`       | Fail if any struct field (by kongfig tag) has no matching key in the merged data. Wraps `mapstructure.ErrorUnused`. |
| `At("dot.path")` | Decode the sub-tree at the given path instead of the root.                                                          |

`Get` uses `mapstructure` with `WeaklyTypedInput: true` (because JSON-decoded numbers arrive as `float64`). Enable `Strict()` only when you can guarantee providers supply correctly typed data.

---

## 4. Render options — `[]RenderOption` / context

Render options are passed as `RenderOption` values to `k.Render`, `k.RenderWith`,
`k.RenderLayers`, and `show.Flags.Render`. They are applied to a typed key-value bag
that is injected into a `context.Context` and passed through to `Renderer.Render(ctx, w, data)`.
Renderers read options via accessors in the `render` sub-package.

| Option                                                  | Effect                                                                                                                                                                                                                                                                                                |
| ------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `WithRenderNoComments()`                                | Suppress all comment output (help texts + source annotations).                                                                                                                                                                                                                                        |
| `WithRenderShowRedacted()`                              | Reveal values that would otherwise be redacted.                                                                                                                                                                                                                                                       |
| `WithRenderFilterSource(filters []string)`              | Source filter list. Empty = show all. `"env"` = only env. `"no-defaults"` = exclude defaults. See [Provenance & Filtering](provenance.md).                                                                                                                                                            |
| `WithRenderHelpTexts(texts map[string]string)`          | Per-path human descriptions emitted as comments above keys. Supports prefix matching (parent path covers map/slice leaves). Each text is emitted at most once per render call. Populate via `schema.HelpTextPaths[T]()`.                                                                              |
| `WithRenderVerboseSources(sources map[string][]string)` | Enables `[env.tag, env.kong]` multi-source annotation expansion.                                                                                                                                                                                                                                      |
| `WithRenderFileRawPaths()`                              | File source annotations show the raw canonical path instead of the display path.                                                                                                                                                                                                                      |
| `WithRenderGroupEnvLayers()`                            | In `RenderLayers`, merge all `env.*` layers into one before iteration.                                                                                                                                                                                                                                |
| `WithRenderFormat(format string)`                       | Output format: `"yaml"`, `"toml"`, `"json"`, `"env"`, `"flags"`.                                                                                                                                                                                                                                      |
| `WithRenderNoAlignSources()`                            | Disable column-alignment of source annotation comments.                                                                                                                                                                                                                                               |
| `WithRenderBlockCollections()`                          | Always render arrays and maps in block/multiline style. Default: inline when short, block when overflowing the terminal width. In TOML, forces `[]ConfigData` slices to use `[[table-array]]` headers; nested `ConfigData` inside elements always forces `[[table-array]]` regardless of this option. |
| `render.WithTTYSize(cols, rows int)`                    | Set terminal dimensions. `AlignAnnotationsCtx` uses this to fall back to above-line annotations when the terminal is too narrow for inline layout.                                                                                                                                                    |

### Context accessors (in `render` package)

Renderers read options via the `render` sub-package, not from a struct:

```go
render.NoComments(ctx)          // bool
render.HelpTexts(ctx)           // map[string]string
render.AlignSources(ctx)        // bool (true = align, default)
render.FilterSourceFromCtx(ctx) // []string
render.FieldName(ctx, path)     // string (field name for current source)
```

### `*Ctx` injection variants

Every render option has a corresponding `With*Ctx` function that injects the
option directly into a `context.Context`. These are used when you cannot pass
`RenderOption` slices — for example when writing a custom renderer or calling
the render path from middleware:

| `*Ctx` function                                        | Equivalent `RenderOption`        |
| ------------------------------------------------------ | -------------------------------- |
| `WithRenderNoCommentsCtx(ctx)`                         | `WithRenderNoComments()`         |
| `WithRenderNoAlignSourcesCtx(ctx)`                     | `WithRenderNoAlignSources()`     |
| `WithRenderFileRawPathsCtx(ctx)`                       | `WithRenderFileRawPaths()`       |
| `WithRenderBlockCollectionsCtx(ctx)`                   | `WithRenderBlockCollections()`   |
| `WithRenderHelpTextsCtx(ctx, texts map[string]string)` | `WithRenderHelpTexts(texts)`     |
| `WithRenderFieldNamesCtx(ctx, names PathFieldNames)`   | _(no RenderOption equivalent)_   |
| `render.WithTTYSizeCtx(ctx, cols, rows int)`           | `render.WithTTYSize(cols, rows)` |

### How show.Flags.Render assembles options

`show.Flags.Render(ctx, w, k, s, opts...)` collects options from:

1. `f.Options(k)` — from CLI flags (`--format`, `--redacted`, `--sources`, `--verbose`, etc.)
2. Caller-provided `opts` — overrides from the application
3. Instance-level settings from `k` (redacted paths, hide flags) — applied by `prepareRender`

---

## 5. Context options — discovery and loading

These are injected into the `context.Context` passed to `Load`, `Watch`, and
file-discovery calls. Providers and discoverers read them to adapt their behaviour.

### Core options (`package kongfig`)

| Function                           | Reader             | Effect                                                                                        |
| ---------------------------------- | ------------------ | --------------------------------------------------------------------------------------------- |
| `WithAppName(ctx, name string)`    | `AppName(ctx)`     | Application name used by file discoverers (e.g. `LocateAppFlat` probes `<dir>/<name>.<ext>`). |
| `WithConfigBase(ctx, base string)` | `ConfigBase(ctx)`  | Base filename for `LocateConfigBase` (default: `"config"`).                                   |
| `WithHiddenFiles(ctx)`             | `HiddenFiles(ctx)` | Also probe hidden variants (`.<appname>.<ext>`, `.<appname>/config.<ext>`) when set.          |

```go
ctx := kongfig.WithAppName(context.Background(), "myapp")
ctx  = kongfig.WithConfigBase(ctx, "settings")
ctx  = kongfig.WithHiddenFiles(ctx)

k.MustLoad(ctx, fileprovider.New(...))
```

### Discovery options (`package discover`)

| Function                    | Reader                   | Effect                                                                                                   |
| --------------------------- | ------------------------ | -------------------------------------------------------------------------------------------------------- |
| `WithLongDisplayPaths(ctx)` | `DisplayPathIsLong(ctx)` | Display absolute/relative paths in `--layers` output instead of short tokens like `$xdg` or `$git-root`. |
