# Migration Guide

## Overview

The migration system of kongfig renames config keys and deprecates file locations across library versions. An existing user config keeps working. When `Load` detects an old key or an old file, a `MigrationEvent` fires. A configurable policy handles the event. That policy can accept the load silently, warn about it, or fail it hard.

Kongfig tracks the migration state (occurrence counts) per instance and across `Load` calls. The first occurrence therefore gets a louder signal, and a repeat is quieter by default.

## Key Renames

Register a rename with `AddRename`:

```go
k.AddRename("database.host", "db.host")
```

On each `Load`, kongfig checks every loaded layer for the old path. There are three cases:

| Old key | New key | Behavior                                               |
| ------- | ------- | ------------------------------------------------------ |
| absent  | any     | no-op. Kongfig skips the rename for this layer         |
| present | absent  | value moved to the new path. `RenameEvent` fired       |
| present | present | old key removed, new value kept. `ConflictEvent` fired |

The value is already at the new path before the handler runs. In the conflict case, `ConflictEvent.OldValue` holds the old value for a log message.

A path is dot-delimited (`"db.host"`). A rename also covers a whole sub-tree (`"database"` → `"db"`).

## Custom Migration Policy

`MigrationPolicy` controls the first detection and every later detection:

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

A production config must hold no deprecated key. Fail hard on the first occurrence, and warn on every later one, for example during a rolling deploy:

```go
strictPolicy := kongfig.MigrationPolicy{
    OnFirst:  kongfig.MigrationFail,  // Load returns error
    OnRepeat: kongfig.MigrationWarn,
}

k.AddRename("database.host", "db.host", strictPolicy)
```

Built-in handlers:

| Handler           | Behavior                    |
| ----------------- | --------------------------- |
| `MigrationSilent` | no-op                       |
| `MigrationDebug`  | `slog.LevelDebug`           |
| `MigrationInfo`   | `slog.LevelInfo`            |
| `MigrationWarn`   | `slog.LevelWarn`            |
| `MigrationFail`   | returns an error. `Load` fails |

## Deprecated File Locations

Wrap an old `Discoverer` with `discover.Deprecated`. A `LegacyFileEvent` then fires for every hit on the deprecated path:

```go
import "github.com/pmarschik/kongfig/providers/file/discover"

file.Discover(ctx,
    discover.Deprecated(discover.XDG(), "~/.config/myapp/config.yaml"),
    yamlparser.Default,
)
```

When the inner discoverer finds a file, `LegacyFileEvent` fires through the policy (default: `DefaultMigrationPolicy`). If the handler returns an error, `Discover` returns that error and loads no file.

`preferredPath` is a human-readable hint for log messages and errors. Discovery does not read it.

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

`Occurrence` is available on every event type, for your own suppression of a repeated message.
