# Load pipeline

## Overview

`kongfig.Load` is the high-level entry point. `kongfig.LoadParsed` is the low-level
entry point for callers that already have a `map[string]any`.

## Call flow

```
Kongfig.Load(ctx, provider, opts...)
  ├─ provider.Load(ctx)                   → map[string]any
  ├─ provider.ProviderName()               → source label
  ├─ collision detection (env.* sources)
  ├─ parser snapshot (ParserProvider)
  ├─ data snapshot (ProviderDataSupport)
  └─ commitLayer(data, source, parser, providerMeta)
       ├─ stamp LayerMeta{ID, Name, Kind, Data}
       ├─ applyTransforms
       ├─ mergeInto(k.data, ...) → SourceMeta per path
       ├─ append Layer{Data, Parser, Meta}
       └─ fire OnLoad hooks
```

## Layer identity model

Each loaded layer carries a `LayerMeta` struct with four fields:

| Field       | Purpose                                                             | Example                        |
| ----------- | ------------------------------------------------------------------- | ------------------------------ |
| `Meta.ID`   | Unique per-Load stamp; ordering reflects load sequence              | `SourceID(3)`                  |
| `Meta.Name` | Stable provenance label (source string passed at load time)         | `"xdg.yaml"`, `"env.tag"`      |
| `Meta.Kind` | Provider category; set by the provider or derived via `inferKind()` | `"file"`, `"env"`              |
| `Meta.Data` | Provider-specific annotation data (file path, env var name)         | `file.SourceData{Path: "..."}` |

`Name` comes from `ProviderName()` (required on `Provider`); `Kind` is set explicitly by
built-in providers and derived via `inferKind(name)` for custom providers; `Data` comes from
`ProviderDataSupport` if the provider implements it.

## LoadParsed options

`LoadParsed(data map[string]any, source string, opts ...LoadOption)` accepts:

- `WithParser(p)` — attaches a parser for native-format rendering in `--layers` mode
  and registers it for `--format` selection.

- `WithProviderData(d)` — attaches `ProviderData` for structured source annotations
  (overrides any `ProviderDataSupport` on the provider).

- `WithSilenceCollisions(keys...)` — suppresses env-collision warnings for env.* sources.
  Pass no keys to silence all warnings for this call.

## Merge strategy

The merge is **last-writer-wins per leaf path**. Each `Load` call overwrites any existing value at the same dot-path; sub-maps are recursed into so sibling keys are preserved. Load order is the only thing that controls priority — later loads beat earlier ones. Array merging and other non-overwrite strategies require an explicit `SetMergeFunc` for the specific path.

## Layer snapshots

After each `Load`, a **deep clone of the provider's post-transform data** is stored as a `Layer` struct. The snapshot is taken before the merge into `k.data` and is never modified afterwards. This is what `--layers` displays — each source's full contribution independent of whether its values survived the merge. Re-loading providers on demand was rejected because providers may have side effects or already be closed.

## Watch reload path

`AddWatcher` snapshots source label, parser, and meta at registration time.
`reloadEntry` forwards them to `LoadParsed` on reload so `--layers` display and
annotations remain consistent after hot-reload.

## Source label conventions

- Env providers: `env.<tag>` prefix (e.g. `env.tag`, `env.prefix`, `env.kong`)
- File providers: `<discoverer>.<format>` (e.g. `xdg.yaml`, `workdir.toml`), Kind=`"file"`
- Flag providers: `flags`
- Defaults: `defaults`
- Derived/computed values: `derived`
