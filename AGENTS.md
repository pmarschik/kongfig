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
- Always try to maintain backward compatibility: add new functions/types instead of changing existing ones
- Deprecate before removing: mark old APIs with `// Deprecated:` and keep them for at least one minor version
- Adding new exported functions, types, or methods is NOT breaking
- Changing function signatures, removing exports, or changing behavior IS breaking

### Code Quality

- Run `mise run check` before pushing
- All linters must pass with zero warnings
- Tests must pass
- Keep README.md up to date when behavior or API changes

### Version Control

- Primary VCS: jj (jujutsu)
- Run `mise run check` before `jj git push`
- Do not push directly — prompt the user (hardware key signing)

## Architecture Quick Reference

See `docs/architecture.md` for the full picture. Key rules for working on this codebase:

### Renderers — two mandatory conventions

1. **Never call `s.Value(formatted)` on a leaf.** Always use:
   ```go
   kongfig.RenderValue(s, v, formattedString)
   ```
   This handles `RedactedValue` centrally. Forgetting this means redacted values render as their raw value.

2. **Never format source annotations inline.** Always use:
   ```go
   line += "  " + s.Comment("# ") + kongfig.RenderSourceAnnotation(src, path, s, opts)
   ```
   This delegates to `LayerMeta.RenderAnnotation` when a meta is registered (structured
   styling with `SourceKind`/`SourceData`/`SourceKey`), and falls back to
   `FormatSourceAnnotation` + `s.Annotation` for sources without meta.

### "Derived" is not computed

`Provenance.SetDerived` / `IsDerived` is a display annotation that lets renderers suppress unchanged defaults. There is **no expression language** — values in the data map are plain Go values. Computed values must be resolved in Go before `Load()`.

### Source label conventions

Env providers **must** use the `env` prefix (e.g. `env.tag`, `env.prefix`) so collision detection and `--layers` grouping work. Other standard labels: `flags`, `file`, `xdg`, `workdir`, `defaults`, `derived`.

### Adding things

- **New renderer**: implement `Renderer`; use `RenderValue` + `RenderSourceAnnotation`; add `Bind(Styler) Renderer` if parser-coupled.
- **New provider**: implement `Provider` (requires `Load` + `ProviderInfo() ProviderInfo`); return `Kind: KindEnv` for env-sourced data; optionally implement `ProviderDataSupport` (`ProviderData() ProviderData`) for rich annotation rendering — env providers return `envprovider.ProviderData{}`, file providers return their `SourceData` struct.
- **New Styler method**: add to `Styler` interface → update `style/plain`, `style/charming`, and `mockStyler` in `interfaces_test.go`.
- **`ProviderData.RenderAnnotation`**: always handle `path=""` gracefully (layer header context has no specific path). Return `""` when there's nothing meaningful to show — `LayerMeta.RenderAnnotation` will omit the parens.
