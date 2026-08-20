# Options Reference

Kongfig has five distinct option types, each one scoped to a different call site.

---

## 1. Kongfig options — `New(opts ...Option)`

Configure the `Kongfig` instance itself. Applied once at construction.

| Type                                     | Effect                                                                                                                                                                                              |
| ---------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `WithLogger{Logger}`                     | Set the `slog.Logger` for internal warnings. Defaults to `slog.Default()`.                                                                                                                          |
| `WithRedacted{Paths}`                    | Register the dot-paths whose values kongfig hides in the rendered output. Typically populated from `structs.RedactedPaths[T]()`.                                                                    |
| `WithRedactionFunc{Fn}`                  | Custom `func(path, value string) string` for redaction display. Default: `"<redacted>"`.                                                                                                            |
| `WithRedactionString(s)`                 | Convenience wrapper for `WithRedactionFunc` — uses a fixed string.                                                                                                                                  |
| `WithHelpTexts(texts map[string]string)` | Register per-path help text emitted as comments above keys, for every render on this instance. Auto-populated by `NewFor[T]` from `help=` struct tags. `WithRenderHelpTexts` overrides it per call. |
| `WithHideAnnotationNames{}`              | Suppress both `$VAR_NAME` and `--flag-name` suffixes from all source annotations.                                                                                                                   |
| `WithHideEnvVarNames{}`                  | Suppress only `$VAR_NAME` suffixes from env source annotations.                                                                                                                                     |
| `WithHideFlagNames{}`                    | Suppress only `--flag-name` suffixes from flags source annotations.                                                                                                                                 |

Kongfig accumulates these options into a `renderConfig`, and applies them at every `Kongfig.Render` or `Kongfig.RenderWith` call.

---

## 2. Load options — `k.Load(provider, opts ...LoadOption)`

Configure a single `Load` call. Applied per-load.

| Type                                         | Effect                                                                                                                                                                                                                 |
| -------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `WithSource{Name}`                           | Override the source label inferred from `provider.Source()`. Use it when you load the same provider type several times and need distinct labels, for example two file providers with `"file.base"` and `"file.local"`. |
| `WithSilenceCollisions{}`                    | Suppress all env collision warnings for this load. Pass no keys to silence all.                                                                                                                                        |
| `WithSilenceCollisions{Keys: []string{...}}` | Suppress collision warnings only for specific dot-paths.                                                                                                                                                               |

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

You pass a render option as a `RenderOption` value to `k.Render`, `k.RenderWith`,
`k.RenderLayers`, or `show.Flags.Render`. Kongfig applies them to a typed key-value bag,
injects that bag into a `context.Context`, and passes it to `Renderer.Render(ctx, w, data)`.
A renderer reads the options through the accessors in the `render` sub-package.

| Option                                                  | Effect                                                                                                                                                                                                                                                                                                     |
| ------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `WithRenderNoComments()`                                | Suppress all comment output (help texts + source annotations).                                                                                                                                                                                                                                             |
| `WithRenderNoProvenance()`                              | Suppress the source annotations only. The help comments stay. Kongfig applies it inside `render.Annotation`, so every renderer honors it.                                                                                                                                                                  |
| `WithRenderNoHelp()`                                    | Suppress the help comments only. The source annotations stay. Kongfig applies it inside `render.HelpText` and `render.HelpTexts`.                                                                                                                                                                          |
| `WithRenderShowRedacted()`                              | Reveal the values that are otherwise redacted.                                                                                                                                                                                                                                                             |
| `WithRenderFilterSource(filters []string)`              | Source filter list. Empty = show all. `"env"` = only env. `"no-defaults"` = exclude defaults. See [Provenance & Filtering](provenance.md).                                                                                                                                                                 |
| `WithRenderHelpTexts(texts map[string]string)`          | Per-path human descriptions emitted as comments above keys. Supports prefix matching (a parent path covers the map leaves and the slice leaves). Kongfig emits each text at most once per render call. Overrides the instance-level set from `WithHelpTexts` / `NewFor[T]`.                                |
| `WithRenderVerboseSources(sources map[string][]string)` | Enables `[env.tag, env.kong]` multi-source annotation expansion.                                                                                                                                                                                                                                           |
| `WithRenderFileRawPaths()`                              | File source annotations show the raw canonical path instead of the display path.                                                                                                                                                                                                                           |
| `WithRenderGroupEnvLayers()`                            | In `RenderLayers`, merge all `env.*` layers into one before iteration.                                                                                                                                                                                                                                     |
| `WithRenderFormat(format string)`                       | Output format: `"yaml"`, `"toml"`, `"json"`, `"env"`, `"flags"`.                                                                                                                                                                                                                                           |
| `WithRenderNoAlignSources()`                            | Disable column-alignment of source annotation comments.                                                                                                                                                                                                                                                    |
| `WithRenderBlockCollections()`                          | Always render arrays and maps in block/multiline style. Default: inline when short, block when overflowing the terminal width. In TOML, forces `[]ConfigData` slices to use `[[table-array]]` headers. A nested `ConfigData` inside an element always forces `[[table-array]]`, whatever this option says. |
| `render.WithTTYSize(cols, rows int)`                    | Set terminal dimensions. `AlignAnnotationsCtx` reads this size, and moves an annotation to the line above the value when the terminal is too narrow for the inline layout.                                                                                                                                 |

### Context accessors (in `render` package)

Renderers read options via the `render` sub-package, not from a struct:

```go
render.NoComments(ctx)          // bool
render.NoProvenance(ctx)        // bool (true under NoComments too)
render.NoHelp(ctx)              // bool (true under NoComments too)
render.HelpTexts(ctx)           // map[string]string
render.AlignSources(ctx)        // bool (true = align, default)
render.FilterSourceFromCtx(ctx) // []string
render.FieldName(ctx, path)     // string (field name for current source)
```

### `*Ctx` injection variants

Every render option has a matching `With*Ctx` function that injects the option
directly into a `context.Context`. Use these functions when you cannot pass a
`RenderOption` slice. Two examples are a custom renderer, and a call into the
render path from middleware:

| `*Ctx` function                                        | Equivalent `RenderOption`        |
| ------------------------------------------------------ | -------------------------------- |
| `WithRenderNoCommentsCtx(ctx)`                         | `WithRenderNoComments()`         |
| `WithRenderNoProvenanceCtx(ctx)`                       | `WithRenderNoProvenance()`       |
| `WithRenderNoHelpCtx(ctx)`                             | `WithRenderNoHelp()`             |
| `WithRenderNoAlignSourcesCtx(ctx)`                     | `WithRenderNoAlignSources()`     |
| `WithRenderFileRawPathsCtx(ctx)`                       | `WithRenderFileRawPaths()`       |
| `WithRenderBlockCollectionsCtx(ctx)`                   | `WithRenderBlockCollections()`   |
| `WithRenderHelpTextsCtx(ctx, texts map[string]string)` | `WithRenderHelpTexts(texts)`     |
| `WithRenderFieldNamesCtx(ctx, names PathFieldNames)`   | _(no RenderOption equivalent)_   |
| `render.WithTTYSizeCtx(ctx, cols, rows int)`           | `render.WithTTYSize(cols, rows)` |

### How show.Flags.Render assembles options

`show.Flags.Render(ctx, w, k, s, opts...)` collects options from:

1. `f.Options(k)` — from CLI flags such as `--format`, `--redacted`, `--sources` and `--verbose`
2. Caller-provided `opts` — overrides from the application
3. Instance-level settings from `k` (redacted paths, hide flags) — applied by `prepareRender`

---

## 5. Context options — discovery and loading

These options go into the `context.Context` that `Load`, `Watch` and the
file-discovery calls receive. A provider or a discoverer reads them and adapts
its behavior.

### Core options (`package kongfig`)

| Function                           | Reader             | Effect                                                                                  |
| ---------------------------------- | ------------------ | --------------------------------------------------------------------------------------- |
| `WithAppName(ctx, name string)`    | `AppName(ctx)`     | Application name for the file discoverers. `LocateAppFlat` probes `<dir>/<name>.<ext>`. |
| `WithConfigBase(ctx, base string)` | `ConfigBase(ctx)`  | Base filename for `LocateConfigBase` (default: `"config"`).                             |
| `WithHiddenFiles(ctx)`             | `HiddenFiles(ctx)` | Also probe hidden variants (`.<appname>.<ext>`, `.<appname>/config.<ext>`) when set.    |

```go
ctx := kongfig.WithAppName(context.Background(), "myapp")
ctx  = kongfig.WithConfigBase(ctx, "settings")
ctx  = kongfig.WithHiddenFiles(ctx)

k.MustLoad(ctx, fileprovider.New(...))
```

### Discovery options (`package discover`)

| Function                    | Reader                   | Effect                                                                                                       |
| --------------------------- | ------------------------ | ------------------------------------------------------------------------------------------------------------ |
| `WithLongDisplayPaths(ctx)` | `DisplayPathIsLong(ctx)` | Show the absolute or relative path in `--layers` output instead of a short token like `$xdg` or `$git-root`. |
