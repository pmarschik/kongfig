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

These are accumulated into a `RenderConfig` inside `Kongfig` and applied to `RenderOptions` at render time via `ApplyRenderConfig(opts, k.RenderConfig())`.

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

## 4. Render options — `RenderOptions` struct

Passed to `Renderer.Render` and assembled by `show.Flags.Render` / `show.SimpleFlags.Render`.

| Field            | Type                              | Effect                                                                                                                                     |
| ---------------- | --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| `HelpTexts`      | `map[string]string`               | Per-path human descriptions emitted as comments above YAML keys.                                                                           |
| `FilterSource`   | `[]string`                        | Source filter list. Empty = show all. `"env"` = only env. `"no-defaults"` = exclude defaults. See [Provenance & Filtering](provenance.md). |
| `HideDerived`    | `bool`                            | Omit leaf values that match their recorded derived (default) baseline.                                                                     |
| `NoComments`     | `bool`                            | Suppress all comment output (help texts + source annotations).                                                                             |
| `RedactedPaths`  | `map[string]bool`                 | Dot-paths whose leaf values are replaced with `RedactedValue`. Applied from `k.RenderConfig()` by `ApplyRenderConfig`.                     |
| `ShowRedacted`   | `bool`                            | When true, skip redaction and show actual values. Controlled by `--redacted`/`--no-redacted`.                                              |
| `RedactFn`       | `func(path, value string) string` | Custom redaction display function. Applied from `k.RenderConfig()`. Default: `"<redacted>"`.                                               |
| `VerboseSources` | `map[string][]string`             | Path → all contributing source labels. Populated from all layers when `--verbose`. Enables `[env.tag, env.kong]` multi-source annotation.  |

Env var names and flag names are stored in the typed bag via [EnvVarNamesKey] and [FlagNamesKey].
The `WithHide*` options (set at `New()` time) nil-out these keys at render time.

### How show.Flags.Render assembles RenderOptions

`show.Flags.Render(w, k, opts, styler)` takes the caller-provided `opts` as a base and fills in:

```
ApplyRenderConfig(opts, k.RenderConfig()) → copies RedactedPaths, RedactFn, applies hide flags
opts.ShowRedacted   ← f.ShowRedacted (from --redacted flag)
opts.VerboseSources ← verboseSources(k.Layers())  (only when f.Verbose > 0)
```

The caller pre-populates `HelpTexts` and sets `EnvVarNamesKey`/`FlagNamesKey` in the bag before calling `Render`.
