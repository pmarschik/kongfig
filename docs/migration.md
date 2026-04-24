# Migration Guide

## Overview

Kongfig's migration system lets you rename config keys and deprecate file locations across library versions without breaking existing user configs. When an old key or file is detected during `Load`, a `MigrationEvent` fires — the event is handled by a configurable policy that can silently accept, warn, or hard-fail the load.

Migration state (occurrence counts) is tracked per `Kongfig` instance across `Load` calls, so the first occurrence gets a louder signal and repeats are quieter by default.

## Key Renames

Register a rename with `AddRename`:

```go
k.AddRename("database.host", "db.host")
```

On each `Load`, kongfig checks every loaded layer for the old path. There are three cases:

| Old key | New key | Behaviour                                              |
| ------- | ------- | ------------------------------------------------------ |
| absent  | any     | no-op; rename is skipped for this layer                |
| present | absent  | value moved to new path; `RenameEvent` fired           |
| present | present | old key dropped; new value kept; `ConflictEvent` fired |

The value is already at the new path before the handler runs. In the conflict case the old value is available via `ConflictEvent.OldValue` if you need to log it.

Paths are dot-delimited (`"db.host"`) and support whole-subtree renames (`"database"` → `"db"`).

## Custom Migration Policy

`MigrationPolicy` controls what happens on the first detection versus subsequent detections:

```go
type MigrationPolicy struct {
    OnFirst  MigrationHandler // fired when Occurrence == 1
    OnRepeat MigrationHandler // fired when Occurrence >= 2
}
```

The default policy warns on the first occurrence and logs at debug level on repeats:

```go
var DefaultMigrationPolicy = MigrationPolicy{
    OnFirst:  MigrationWarn,
    OnRepeat: MigrationDebug,
}
```

To enforce that no deprecated keys remain in production configs, fail hard on first occurrence and warn on subsequent ones (e.g. during a rolling deploy):

```go
strictPolicy := kongfig.MigrationPolicy{
    OnFirst:  kongfig.MigrationFail,  // Load returns error
    OnRepeat: kongfig.MigrationWarn,
}

k.AddRename("database.host", "db.host", strictPolicy)
```

Built-in handlers:

| Handler           | Behaviour                   |
| ----------------- | --------------------------- |
| `MigrationSilent` | no-op                       |
| `MigrationDebug`  | `slog.LevelDebug`           |
| `MigrationInfo`   | `slog.LevelInfo`            |
| `MigrationWarn`   | `slog.LevelWarn`            |
| `MigrationFail`   | returns error; `Load` fails |

## Deprecated File Locations

Wrap an old `Discoverer` with `discover.Deprecated` to fire a `LegacyFileEvent` whenever the deprecated path is found:

```go
import "github.com/pmarschik/kongfig/providers/file/discover"

file.Discover(ctx,
    discover.Deprecated(discover.XDG(), "~/.config/myapp/config.yaml"),
    yamlparser.Default,
)
```

When the inner discoverer finds a file, `LegacyFileEvent` fires through the policy (default: `DefaultMigrationPolicy`). If the handler returns an error, `Discover` returns that error and the file is not loaded.

`preferredPath` is a human-readable hint shown in log messages and errors — it is not used for discovery.

To supply a custom policy:

```go
discover.Deprecated(
    discover.XDG(),
    "~/.config/myapp/config.yaml",
    kongfig.MigrationPolicy{
        OnFirst:  kongfig.MigrationFail,
        OnRepeat: kongfig.MigrationSilent,
    },
)
```

## Custom Handlers

`MigrationHandler` is `func(MigrationEvent) error`. Use a type switch to inspect the event:

```go
migrationHint := func(e kongfig.MigrationEvent) error {
    switch ev := e.(type) {
    case kongfig.RenameEvent:
        fmt.Fprintf(os.Stderr, `{"event":"rename","old":%q,"new":%q,"source":%q}`+"\n",
            ev.OldPath, ev.NewPath, ev.SourceName)
    case kongfig.ConflictEvent:
        fmt.Fprintf(os.Stderr, `{"event":"conflict","old":%q,"new":%q,"source":%q}`+"\n",
            ev.OldPath, ev.NewPath, ev.SourceName)
    case kongfig.LegacyFileEvent:
        fmt.Fprintf(os.Stderr, `{"event":"legacy_file","path":%q,"preferred":%q}`+"\n",
            ev.FilePath, ev.PreferredPath)
    }
    return nil
}

k.AddRename("database.host", "db.host", kongfig.MigrationPolicy{
    OnFirst:  migrationHint,
    OnRepeat: kongfig.MigrationSilent,
})
```

`Occurrence` is available on every event type if you need to suppress repeated output yourself.
