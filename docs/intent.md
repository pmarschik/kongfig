<!-- read_when: starting work on this repo; planning new features; clarifying scope -->

# Intent

## What kongfig is

A layered configuration library for Go CLIs. The core proposition: not just "load config
from multiple sources" but _show the user exactly where each value came from_ — via a
`show-config`-style command that renders annotated output in each source's native format.

Most config libraries answer "what is the value of X?".
Kongfig also answers "which source set X, and what did every other source contribute?".

## Design principles

- **Layers are preserved, not just merged.** Each `Load` call records a snapshot. The
  merged view is derived; the layer snapshots are primary and always inspectable.
- **Provenance is a first-class citizen.** Every leaf value tracks its source label.
  Renderers annotate values inline; the user sees `host: prod.example.com  # env $APP_HOST`.
- **Rendering is part of the public API.** `Renderer`, `Styler`, `OutputProvider`, and
  `LayerMeta` are public interfaces. Consumers extend them; the library doesn't own output.
- **Format fidelity.** A YAML file renders as YAML, TOML as TOML. `Layer.Parser` carries
  the native format; per-layer rendering uses it automatically.
- **kong-aware, not kong-required.** The core module has zero kong dependency. The
  `kong/provider`, `kong/show`, `kong/resolver`, `kong/charming` modules are opt-in.

## CLI behavior: `show-config` intent

The `kong/show.Flags` struct is designed to be embedded in any CLI's `show-config`
(or `--show-config`) command. The intended flag combinations and their behaviors:

### Merged view (default)

No `--layers` flag. Renders the single merged map.

| Flags           | Behavior                                                                                                                                                          |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| _(none)_        | Merged config; format chosen from registered parsers (first registered, or YAML if none); inline `# source` annotations on each value.                            |
| `--format=X`    | Force a specific output format (yaml/toml/json/env/flags). Overrides the inferred default. Not meaningful in `--layers` mode (each layer uses its native format). |
| `--no-comments` | Suppress source annotations.                                                                                                                                      |
| `--redacted`    | Reveal values that are normally hidden (e.g. passwords).                                                                                                          |

**Note:** Default format selection (inferring YAML vs. TOML from registered parsers) is
planned but not yet implemented. Currently always defaults to YAML.

### Per-layer view (`--layers`)

Renders each config source as a separate section. Each layer uses its native format.
`--format` is not meaningful here (native format wins); it acts only as a fallback for
layers with no native renderer (e.g. `defaults`, `derived`).

| Flags                    | Behavior                                                                                                               |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------- |
| `--layers`               | One section per source group. All `env.*` sub-sources merged into a single `env` section.                              |
| `--layers -v`            | One section per sub-source. Each env provider shown separately: `# === env (env.tag) ===`, `# === env (env.kong) ===`. |
| `--layers --no-defaults` | Exclude the defaults layer.                                                                                            |
| `--layers --no-env`      | Exclude env layers.                                                                                                    |
| `--layers --no-file`     | Exclude file/xdg/workdir layers.                                                                                       |
| `--layers --no-flags`    | Exclude the flags layer.                                                                                               |

The `--no-<source>` flags are **not built into `kong/show.Flags`**. Applications wire them
manually using `BuildFilterSource` (see `example/common` and `example/full`). Two variants
are possible: negatable boolean flags per source (`--no-defaults`, `--no-env`, …) or a
single `--sources=defaults,env,file,flags` list flag. Neither is in `kong/show` yet;
tracked as a future feature.

### Source annotation detail (`-v` / `--verbose`)

`--verbose` / `-v` is a counter flag:

- **0 (default):** `env.*` sub-sources collapsed to `env` in layer headers; per-value
  annotations show `# env $APP_HOST`.
- **1+ (`-v`):** Sub-source labels shown in full (`env (env.tag)`, `env (env.kong)`).
  Planned: additional levels may show more annotation detail.

### `--plain` / `--no-plain`

Available on `SimpleFlags`. Disables ANSI color output. When `Flags` is used, the caller
controls styling by passing the appropriate `Styler` to `Render`.

### `--redacted` / `--no-redacted`

`--no-redacted` (default): values at paths registered via `kongfig.WithRedacted` or
`kongfig.NewFor[T]()` are replaced with `<redacted>` in all output.
`--redacted`: reveals the raw values.
