# Validation

The `kongfig/validation` package is an optional layer on top of the core `kongfig` package.
It provides per-key validators, cross-key validators, and schema annotation validators
with configurable per-load or post-load execution.

## Concepts

| Concept              | What it does                                                                          |
| -------------------- | ------------------------------------------------------------------------------------- |
| `Validator`          | Central registry; accumulates validator definitions and wires into `*kongfig.Kongfig` |
| `AddValidator`       | Per-key validation: fires for one dot-path                                            |
| `Rule[T]`            | Cross-key validation: decodes a typed projection struct and validates relationships   |
| `Schema[T]`          | Annotation-driven validation: reads `kongfig` struct tags to install validators       |
| `RegisterAnnotation` | Extend the annotation system with custom tag options                                  |
| `Severity`           | `Error` / `Warning` / `Info` / `Hint` — only `Error` causes `Err()` to return non-nil |
| `Diagnostics`        | Bag of violations returned by `Validate()`; call `.Err()` for a plain `error`         |

---

## Quick start

```go
v := validation.NewWithDefaults()

// Per-key validator
v.AddValidator("port", func(e validation.Event) []validation.FieldViolation {
    if n, ok := e.Value.(int); ok && (n < 1 || n > 65535) {
        return []validation.FieldViolation{{Message: "port must be 1–65535", Code: "port.range"}}
    }
    return nil
})

// Cross-key rule — projection struct tags are full dot-paths from root
type DBConnRule struct {
    MinConns int `kongfig:"db.min_conns"`
    MaxConns int `kongfig:"db.max_conns"`
}
v.AddRule(validation.Rule(
    func(c DBConnRule) []validation.FieldViolation {
        if c.MaxConns < c.MinConns {
            return []validation.FieldViolation{{Message: "max_conns must be >= min_conns"}}
        }
        return nil
    },
))

// Run after all layers are loaded
d := v.Validate(k)
if err := d.Err(); err != nil {
    log.Fatal(err)
}
```

---

## Per-key validators

```go
v.AddValidator(path string, fn validation.Func)
```

- `fn` receives `Event{Key, Value}` and returns `[]FieldViolation`.
- The validator is skipped silently when the key is absent from the merged config.
- Multiple validators may be registered for the same path; all run.

To fire all validators on every `Load()`, pass `WithValidateOnLoad` at construction time
(see [ValidateOnLoad — global switch](#validateonload--global-switch) below).

---

## Cross-key rules

```go
r := validation.Rule(fn func(T) []validation.FieldViolation) RuleValidator
v.AddRule(r)
```

`Rule[T]` decodes the Kongfig into `T` via `kongfig.GetByPaths[T]` and calls `fn`.
Each field in `T` must use its `kongfig` tag as a **full dot-delimited path from the
config root** — no `At()` prefix is accepted or needed:

```go
type DBConnRule struct {
    MinConns int `kongfig:"db.min_conns"` // absolute path from root
    MaxConns int `kongfig:"db.max_conns"`
}
```

Violation paths are inferred automatically from `T`'s leaf tags.

`RuleValidator` is opaque — construct it only with `Rule`.

### Composite rule helpers

The `validation` package ships helpers for common multi-field constraints. Pass field
pointers within the decoded struct `T` — paths are derived from each field's `kongfig`
tag automatically:

| Helper                              | Constraint                                        |
| ----------------------------------- | ------------------------------------------------- |
| `ExactlyOneOf(&c, &c.A, &c.B)`      | Exactly one field must be non-zero                |
| `AtLeastOneOf(&c, &c.A, &c.B)`      | One or more fields must be non-zero               |
| `MutuallyExclusive(&c, &c.A, &c.B)` | At most one field may be non-zero                 |
| `AllOrNone(&c, &c.A, &c.B)`         | Either all are non-zero, or none are              |
| `RequiredWith(&c, &c.F, &c.Trig)`   | `F` required when any trigger field is non-zero   |
| `RequiredWithout(&c, &c.F, &c.FB)`  | `F` required when none of the fallback fields are |

```go
type TLSConfig struct {
    CertFile string `kongfig:"tls.cert-file"`
    KeyFile  string `kongfig:"tls.key-file"`
}

v.AddRule(validation.Rule(func(c TLSConfig) []validation.FieldViolation {
    return validation.AllOrNone(&c, &c.CertFile, &c.KeyFile)
}))
```

All helpers panic if a pointer cannot be matched to a field in `*T` (programming error).

---

## Registry

Annotation handlers are stored in a `Registry`. Choose the right constructor:

| Constructor                 | Semantics                                                                     |
| --------------------------- | ----------------------------------------------------------------------------- |
| `DefaultRegistry()`         | The package-level shared registry; `NewWithDefaults()` holds a live reference |
| `NewRegistryFromDefaults()` | Independent copy seeded from current `DefaultRegistry()` handlers             |
| `NewEmptyRegistry()`        | Blank — no handlers, not even `"required"`                                    |

### Validator constructors

| Constructor         | Registry                      | Returns               | nil arg |
| ------------------- | ----------------------------- | --------------------- | ------- |
| `NewWithDefaults()` | live → `DefaultRegistry`      | `*Validator`          | n/a     |
| `NewFromDefaults()` | snapshot of `DefaultRegistry` | `*Validator`          | n/a     |
| `NewWith(reg)`      | live → `reg`                  | `(*Validator, error)` | error   |
| `NewFrom(reg)`      | snapshot copy of `reg`        | `(*Validator, error)` | error   |
| `NewEmpty()`        | none                          | `*Validator`          | n/a     |

**Live** means annotations added to the registry after construction are visible on the
next `Validate()` call. **Snapshot** means the registry is copied at construction time —
later changes to the source registry are invisible.

```go
// Simple apps: register globally once, all NewWithDefaults()-based validators see it.
validation.RegisterAnnotation("nonempty", myHandler)

// Isolated registry — changes to DefaultRegistry after construction are not visible.
reg := validation.NewRegistryFromDefaults()
reg.Register("nonempty", myHandler)
v, err := validation.NewWith(reg)

// Live reference to a custom registry — adds after NewWith are visible.
reg := validation.NewEmptyRegistry()
v, err := validation.NewWith(reg)
reg.Register("nonempty", myHandler) // still visible to v
```

---

## Schema annotation validators

`Schema[T]` reflects on `T`'s `kongfig` struct tags and extracts annotation options
(anything after the first comma that isn't a structural option like `squash` or `redacted`).
`AddSchema` stores the schema for lazy resolution at `Validate()` time — call order
of `AddSchema` and `Registry.Register` does not matter.

```go
type Config struct {
    Host string `kongfig:"host,required"`
    Port int    `kongfig:"port"`
}

v := validation.NewWithDefaults()             // uses DefaultRegistry live; "required" pre-wired
v.AddSchema(validation.Schema[Config]()) // stored for lazy resolution
```

`SchemaValidator` is opaque — construct it only with `Schema`.

### At() prefix

```go
v.AddSchema(validation.Schema[DBConfig](kongfig.At("db")))
```

Prepends `"db."` to all extracted paths.

### Built-in annotations

| Annotation | Effect                                                      |
| ---------- | ----------------------------------------------------------- |
| `required` | Missing-key check for the field; `Code: "kongfig.required"` |

### Custom annotations

Use `AnnotationFieldFunc` for the common field-scoped case — the framework resolves
the field value and sets `Paths` on violations automatically:

```go
reg := validation.NewRegistryFromDefaults()
reg.Register("nonempty", validation.AnnotationFieldFunc(
    func(e validation.AnnotationEvent) []validation.FieldViolation {
        if !e.Exists {
            return nil // absent key; "required" handles that separately
        }
        if s, ok := e.Value.(string); ok && s == "" {
            return []validation.FieldViolation{{
                Message:  e.Path + " must not be empty",
                Code:     "nonempty",
                Severity: validation.SeverityError,
            }}
        }
        return nil
    },
))
v, err := validation.NewWith(reg)
if err != nil {
    log.Fatal(err)
}
v.AddSchema(validation.Schema[Config]())
```

`AnnotationEvent` fields:

| Field    | Type       | Meaning                                                        |
| -------- | ---------- | -------------------------------------------------------------- |
| `Path`   | `string`   | Full dot-delimited config key                                  |
| `Args`   | `[]string` | Expression arguments; nil for zero-arg atoms (e.g. `required`) |
| `Value`  | `any`      | Current value at `Path`; only valid when `Exists` is true      |
| `Exists` | `bool`     | Whether `Path` is present in the merged config                 |

`Args` reflects the validate= expression: `required` → `nil`, `min(1)` → `["1"]`,
`oneof(a b c)` → `["a", "b", "c"]`.

For cross-field constraints use `Rule` instead of annotation handlers.

### Annotation param helpers

Use these inside `AnnotationFieldFunc` to parse `e.Args`:

```go
n, ok  := validation.ParseParamInt(e.Args[0])  // parse first arg as integer
b, ok  := validation.ParseParamBool(e.Args[0]) // parse first arg as boolean
items  := validation.ParseParamList(e.Args[0]) // split pipe-separated list
```

Unknown annotation tags surface as `SeverityError{Code: "kongfig.unknown_annotation"}`
when `Validate()` is called, not silently dropped at `AddSchema` time.

---

## OnLoad transactional semantics

`OnLoad` hooks run on the **proposed** merged state before it is committed. If any hook returns a non-nil error, the load is rejected: `k.data`, `k.prov`, and `k.layers` are left unchanged. Inside a hook, `k.Raw()` returns the pre-load state; use `e.ProposedData` to inspect the post-merge view. Per-load field validators only fire for keys present in the current layer's data (`e.Layer.Data`), not all keys in the merged config — a stale bad value from a previous layer cannot cause a later, unrelated load to fail. `SeverityError` violations reject the load entirely; lower-severity violations accumulate in `Diagnostics.LoadViolations` for `Validate()` to surface.

## ValidateOnLoad — global switch

`WithValidateOnLoad(at Severity)` makes **all** field validators fire on every `Load()`.
Violations at severity `at` or above cause `Load()` to return an error immediately;
lower-severity violations are accumulated in `Diagnostics.LoadViolations`.

```go
v := validation.NewWithDefaults(validation.WithValidateOnLoad(validation.SeverityError))
```

This is useful in strict environments where any invalid load should fail fast.

---

## Wiring into Kongfig

```go
v.Register(k)
```

Installs an `OnLoad` hook on `k`. The hook only fires if `WithValidateOnLoad` was set
at construction time.
Calling `Register` on the same `k` more than once is a no-op — the hook is installed
exactly once regardless.
Call `Register` after all validators are added and before any `Load` calls.

---

## Validate — post-load check

```go
d := v.Validate(k)
```

Runs all validators (per-key, cross-key, required checks) against the current merged
state of `k`. Also drains any per-load violations accumulated since the last `Validate`
call, attaching them as `Diagnostics.LoadViolations`.

Returns `nil` when there are no violations of any severity.

---

## Diagnostics

```go
type Diagnostics struct {
    Violations     []Violation      // from Validate()
    LoadViolations []LayerViolation // accumulated from per-load hook firings
}

func (d *Diagnostics) Err() error
```

- `Err()` returns a non-nil error only when at least one `SeverityError` violation exists.
- `Err()` is nil-safe: `(*Diagnostics)(nil).Err()` returns `nil`.
- `LoadViolations` links each violation set to the `kongfig.Layer` that triggered it.

### Severity

| Level             | `Err()` non-nil | Meaning                          |
| ----------------- | --------------- | -------------------------------- |
| `SeverityError`   | yes             | Config is unusable               |
| `SeverityWarning` | no              | Should fix, but app can continue |
| `SeverityInfo`    | no              | Informational only               |
| `SeverityHint`    | no              | Optional improvement suggestion  |

---

## Execution order

```
k.Load()  (each call)
  └─ OnLoad hook (if Register was called and WithValidateOnLoad was set)
       └─ per-key field validators (all registered paths)
            → violations at or above cutoff severity cause Load() to fail
            → lower-severity violations accumulate in Validator

v.Validate(k)
  ├─ per-key field validators (all, against merged state)
  ├─ schema annotation validators (lazy: resolved against registry handlers;
  │    unknown tags → SeverityError)
  ├─ cross-key rules (Rule[T] via GetByPaths[T])
  └─ drain accumulated LoadViolations
       → returns *Diagnostics (nil if no violations)
```
