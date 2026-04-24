# Live Reload / Watch

## Overview

`Watch` enables live config reload without restarting the process. When a
watched provider detects a change it reloads its data, merges it into the
`Kongfig` instance, and fires your `OnChange` callbacks — all while the
application keeps running.

---

## Wiring up watch

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

fp := fileprovider.New("config.yaml", yamlparser.Default)
kf := kongfig.New()
kf.MustLoad(ctx, fp)        // initial load
kf.AddWatcher(fp)           // register fp as a live-reload source
kf.OnChange(func() {
    cfg, _ := kongfig.Get[AppConfig](kf)
    log.Println("config reloaded:", cfg)
})
go kf.Watch(ctx)            // start watching in the background
```

| Call             | Purpose                                                                                     |
| ---------------- | ------------------------------------------------------------------------------------------- |
| `AddWatcher(wp)` | Register a `WatchProvider` to be started by `Watch`. Call once per provider before `Watch`. |
| `OnChange(fn)`   | Register a callback invoked after every successful reload. Multiple callbacks are allowed.  |
| `Watch(ctx)`     | Start all registered watchers and block until `ctx` is canceled. Returns any watcher error. |

`Watch` blocks, so run it in a goroutine (or your application's shutdown
loop). Cancel the context to stop all watchers cleanly.

---

## Handling WatchEvent

Providers deliver notifications via a callback of type `WatchFunc`:

```go
type WatchFunc func(WatchEvent)
```

`WatchEvent` is a sealed sum type — use a type switch:

```go
func handleEvent(ev kongfig.WatchEvent) {
    switch e := ev.(type) {
    case kongfig.WatchDataEvent:
        // e.Data holds the freshly loaded ConfigData
        cfg, _ := kongfig.Get[AppConfig](kf)
        _ = cfg
    case kongfig.WatchErrorEvent:
        log.Printf("watch error: %v", e.Err)
    }
}
```

`kongfig.Watch` wires this up internally — you rarely call `WatchFunc`
directly. Use `OnChange` for successful-reload reactions; the error case is
available if you implement your own `WatchProvider`.

---

## Implementing WatchProvider

`WatchProvider` extends `Provider` with a single method:

```go
type WatchProvider interface {
    Provider
    Watch(ctx context.Context, cb WatchFunc) error
}
```

Contract:

- **Block until `ctx` is canceled.** `Watch` must not return early unless an
  unrecoverable error occurs. Transient errors (e.g. a temporarily missing
  file) should be delivered as `WatchErrorEvent` and allow the watch loop to
  continue.
- **Call `cb(WatchDataEvent{Data: data})` on each successful reload.** `data`
  is the full `ConfigData` map for this provider's contribution (same shape as
  `Provider.Load` returns).
- **Call `cb(WatchErrorEvent{Err: err})` on errors.** Do not return from
  `Watch` for recoverable errors — deliver them via the callback and keep
  watching.
- **Return `nil` (or a meaningful error) when `ctx` is canceled.** Returning
  `ctx.Err()` is acceptable; `kongfig.Watch` discards `context.Canceled`.

Minimal skeleton:

```go
func (p *MyProvider) Watch(ctx context.Context, cb kongfig.WatchFunc) error {
    for {
        select {
        case <-ctx.Done():
            return nil
        case <-p.changeSignal:
            data, err := p.load()
            if err != nil {
                cb(kongfig.WatchErrorEvent{Err: err})
                continue
            }
            cb(kongfig.WatchDataEvent{Data: data})
        }
    }
}
```

---

## File provider watch

`providers/file` implements `WatchProvider` out of the box using
[fsnotify](https://github.com/fsnotify/fsnotify). File modifications trigger
an automatic reload and fire `OnChange` callbacks.

Usage is the same as shown in the [Wiring up watch](#wiring-up-watch) section
above — pass the file provider to both `MustLoad` and `AddWatcher`:

```go
fp := fileprovider.New("config.yaml", yamlparser.Default)
kf.MustLoad(ctx, fp)
kf.AddWatcher(fp)
```

No extra setup is required. The watcher starts when `kf.Watch(ctx)` is called
and stops when the context is canceled.
