# Provenance & Filtering

## What is provenance?

`Provenance` records, for each leaf dot-path in the merged config, which source last wrote it:

```
path       → source label
"host"     → "env.tag"
"port"     → "flags"
"log.level" → "file"
"timeout"  → "defaults"
```

It is updated on every `mergeInto` call (i.e. on every `Load`). The last writer wins — earlier sources' provenance for a path is overwritten.

`k.Provenance()` returns a snapshot copy safe for concurrent use.

## Derived values

`Provenance` also holds a `derived` map: `path → baseline string value`.

`SetDerived(path, val)` records the canonical default for a path. `IsDerived(path, val)` checks whether the current live value matches it.

This is purely a **display annotation**. There is no expression language or computed-value machinery. It exists so renderers can suppress "boring" lines — values that are still at their default — when `HideDerived: true` is set in `RenderOptions`.

Who calls `SetDerived`? Any provider that wants to communicate "this is the baseline" — typically a defaults provider. The structs `TagDefaults` provider does not currently call it; callers can call it manually via `k.Provenance().SetDerived(path, val)` or by giving `kongfig.NewProvenance()` a `SetDerived` call before `LoadParsed`.

## Source labels

Source labels are the canonical short names used in provenance and layer filtering:

| Label        | Meaning                                  |
| ------------ | ---------------------------------------- |
| `defaults`   | Struct or file defaults                  |
| `file`       | Config file                              |
| `xdg`        | XDG config file                          |
| `workdir`    | Workdir-local config file                |
| `env`        | Environment variables (generic)          |
| `env.tag`    | Struct tag env vars (`env:""` field tag) |
| `env.kong`   | Env vars read by kong flag resolver      |
| `env.prefix` | Prefix-stripped env vars                 |
| `flags`      | Explicit CLI arguments                   |
| `derived`    | Computed/inherited values                |

Env sub-sources share the `env` prefix. This enables:

- Collision detection: any two `env.*` providers writing the same path triggers a warning.
- Merged display: `env.*` providers collapse to `"env"` in the merged view (non-verbose mode).
- Filtering: `FilterSource: []string{"env"}` matches `env`, `env.tag`, `env.kong`, `env.prefix`.

## Source label design

Source labels are plain strings. A typed enum was rejected because it would prevent third-party providers from introducing custom labels (e.g. `"consul"`, `"vault"`). A structured type was rejected because the `env.` prefix already encodes the one case where structural matching matters. The `env.*` prefix is the only convention with runtime significance — everything else is advisory. A provider that uses `"environment"` instead of `"env"` will not participate in collision detection or `env` layer grouping; this is enforced by documentation, not the type system.

## FilterSource semantics

`RenderOptions.FilterSource` is a list of filter entries. Empty = no filter (show all).

**Exclude entries** (`no-` prefix): exclude any source matching the prefix.

- `"no-defaults"` excludes `"defaults"`
- `"no-env"` excludes `"env"`, `"env.tag"`, `"env.kong"`, `"env.prefix"`

**Include entries** (no prefix): when any positive entry is present, only matching sources pass.

- `"env"` allows only `"env"`, `"env.tag"`, `"env.kong"`, `"env.prefix"`
- `"flags"` allows only `"flags"`

Both can be combined: `["env", "no-env.tag"]` = all env sub-sources except `env.tag`.

**Prefix matching**: `"env"` matches `source == "env"` OR `strings.HasPrefix(source, "env.")`.

`BuildFilterSource(map[string]bool)` builds a filter list from a map of `layerName → show`.

## Layers vs merged provenance

`k.Layers()` returns each provider's snapshot in load order. Each `Layer.Data` is the pre-merge data for that provider (not the merged result). This is what `--layers` displays.

The merged `k.data` reflects last-writer-wins across all layers; its provenance is in `k.Provenance()`.

These serve different purposes:

- **Merged view**: "what is the current effective value and who last set it?"
- **Layer view**: "what did each provider contribute, independent of priority?"
