# kongfig

## Build & Test Commands

- `mise run setup` — install dependencies
- `mise run check` — run all quality gates (format + lint + test)
- `mise run fmt` — format code
- `mise run lint` — run linters
- `mise run test` — run tests
- `go build ./...` — build library

## Conventions

### Commits

Use Conventional Commits strictly:

    <type>(<scope>): <description>

Types: feat, fix, refactor, build, ci, chore, docs, style, perf, test
Scopes: defined in `cog.toml` — update that file when adding new scopes.

Current scopes:

- `core` — root package / load-get-render pipeline / shared public API glue
- `render` — `render/`
- `validation` — `validation/`
- `mergefuncs` — `mergefuncs/`
- `parsers` — cross-parser work spanning multiple parser packages
- `parsers/json` — `parsers/json`
- `parsers/yaml` — `parsers/yaml`
- `parsers/toml` — `parsers/toml`
- `providers` — cross-provider work spanning multiple provider packages
- `providers/env` — `providers/env`
- `providers/file` — `providers/file`
- `providers/structs` — `providers/structs`
- `style` — cross-style work spanning multiple styling packages
- `style/plain` — `style/plain`
- `style/charming` — `style/charming`
- `kong` — cross-kong integration work spanning multiple `kong/*` packages
- `kong/resolver` — `kong/resolver`
- `kong/provider` — `kong/provider`
- `kong/show` — `kong/show`
- `kong/charming` — `kong/charming`
- `examples` — runnable example modules under `example/`
- `docs` — README, docs, ADRs
- `build` — local tooling, `mise`, scripts, developer setup
- `ci` — GitHub Actions and CI automation
- `release` — release flow, changelog, versioning, `cog.toml`, git-cliff/cocogitto config

`cog` accepts slash scopes when they are listed in `cog.toml`.

Prefer the narrowest matching scope. Use the coarse umbrella scopes (`parsers`, `providers`, `style`, `kong`) when a change intentionally spans several subcomponents. Use the meta scopes only when the change is primarily repo/process work rather than a package behavior change.

Every commit MUST follow this format. The CI pipeline enforces this via git-cliff.

### API Stability

This is a public Go library. Breaking changes affect downstream consumers.

- **NEVER introduce breaking API changes without asking the user first**
- Breaking changes MUST use `feat!:` or `fix!:` commit prefix (triggers major version bump)
- Always keep backward compatibility. Add a new function or type instead of a change to an existing one
- Deprecate an API before you remove it. Mark it with `// Deprecated:` and keep it for at least one minor version
- A new exported function, type, or method is NOT breaking
- These three changes ARE breaking: a change to a function signature, the removal of an export, a change in behavior

### Code Quality

- Run `mise run check` before pushing
- All linters must pass with zero warnings
- Tests must pass
- When behavior or the API changes, update README.md

### Version Control

- Primary VCS: jj (jujutsu)
- Run `mise run check` before `jj git push`
- Do not push directly — prompt the user (hardware key signing)

## Architecture Quick Reference

See `docs/architecture.md` for the full picture. Key rules for this codebase:

### Renderers — two mandatory conventions

1. **Never call `s.String(formatted)` on a leaf.** Always use:
   ```go
   render.Value(s, v, formattedString)
   ```
   This function handles `RenderedValue` centrally (redaction, codec styling, type dispatch). If you forget it, a redacted value renders as its raw value.

2. **Never format source annotations inline.** Always use:
   ```go
   if ann := render.Annotation(ctx, rv, path, s); ann != "" {
       line += "  " + s.Comment("# ") + ann
   }
   ```
   `render.Annotation` delegates to `LayerMeta.RenderAnnotation` (structured styling with
   `SourceKind`/`SourceData`/`SourceKey`) and returns `""` when there is no source or
   `render.NoComments(ctx)` is true — no separate check needed.

### "Derived" is not computed

`Provenance.SetDerived` and `IsDerived` form a display annotation. A renderer uses it to suppress an unchanged default. There is **no expression language** — every value in the data map is a plain Go value. Resolve a computed value in Go before you call `Load()`.

### Source label conventions

An env provider **must** use the `env` prefix, such as `env.tag` and `env.prefix`. Collision detection and `--layers` grouping need that prefix. The other standard labels are `flags`, `file`, `xdg`, `workdir`, `defaults` and `derived`.

### Adding things

- **New renderer**: implement `Renderer`. Use `RenderValue` and `RenderSourceAnnotation`. If the renderer is parser-coupled, add `Bind(Styler) Renderer`.
- **New provider**: implement `Provider`. It requires `Load` and `ProviderInfo() ProviderInfo`. Return `Kind: KindEnv` for env-sourced data. For rich annotation rendering, also implement `ProviderDataSupport` (`ProviderData() ProviderData`). An env provider returns `envprovider.ProviderData{}`, and a file provider returns its `SourceData` struct.
- **New Styler method**: add to `Styler` interface → update `style/plain`, `style/charming`, and `mockStyler` in `interfaces_test.go`.
- **`ProviderData.RenderAnnotation`**: always handle `path=""` (the layer header context has no specific path). Return `""` when there is nothing to show. `LayerMeta.RenderAnnotation` then omits the parens.
