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

| Option                                                  | Effect                                                                                                                                     |
| ------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| `WithRenderNoComments()`                                | Suppress all comment output (help texts + source annotations).                                                                             |
| `WithRenderShowRedacted()`                              | Reveal values that would otherwise be redacted.                                                                                            |
| `WithRenderFilterSource(filters []string)`              | Source filter list. Empty = show all. `"env"` = only env. `"no-defaults"` = exclude defaults. See [Provenance & Filtering](provenance.md). |
| `WithRenderHelpTexts(texts map[string]string)`          | Per-path human descriptions emitted as comments above keys.                                                                                |
| `WithRenderVerboseSources(sources map[string][]string)` | Enables `[env.tag, env.kong]` multi-source annotation expansion.                                                                           |
| `WithRenderFileRawPaths()`                              | File source annotations show the raw canonical path instead of the display path.                                                           |
| `WithRenderGroupEnvLayers()`                            | In `RenderLayers`, merge all `env.*` layers into one before iteration.                                                                     |
| `WithRenderFormat(format string)`                       | Output format: `"yaml"`, `"toml"`, `"json"`, `"env"`, `"flags"`.                                                                           |
| `WithRenderNoAlignSources()`                            | Disable column-alignment of source annotation comments.                                                                                    |

### Context accessors (in `render` package)

Renderers read options via the `render` sub-package, not from a struct:

```go
render.NoComments(ctx)        // bool
render.HelpTexts(ctx)         // map[string]string
render.AlignSources(ctx)      // bool (true = align, default)
render.FilterSourceFromCtx(ctx) // []string
render.FieldName(ctx, path)   // string (field name for current source)
```

### How show.Flags.Render assembles options

`show.Flags.Render(ctx, w, k, s, opts...)` collects options from:

1. `f.Options(k)` — from CLI flags (`--format`, `--redacted`, `--sources`, `--verbose`, etc.)
2. Caller-provided `opts` — overrides from the application
3. Instance-level settings from `k` (redacted paths, hide flags) — applied by `prepareRender`
