# Live Reload / Watch

## Overview

`Watch` reloads the config while the process runs. When a watched provider
detects a change, it reloads its data. It then merges that data into the
`Kongfig` instance, and fires your `OnChange` callbacks. The application keeps
running throughout.

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
| `AddWatcher(wp)` | Register a `WatchProvider` for `Watch` to start. Call once per provider before `Watch`.     |
| `OnChange(fn)`   | Register a callback that runs after every successful reload. Several callbacks are legal.   |
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

`kongfig.Watch` wires this internally, and you rarely call `WatchFunc`
directly. Use `OnChange` for a reaction to a successful reload. The error case
is available when you implement your own `WatchProvider`.

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

- **Block until `ctx` is canceled.** `Watch` must not return early, except on
  an unrecoverable error. Deliver a transient error as `WatchErrorEvent`, for
  example a temporarily missing file. The watch loop must then continue.
- **Call `cb(WatchDataEvent{Data: data})` on each successful reload.** `data`
  is the full `ConfigData` map for this provider's contribution (same shape as
  `Provider.Load` returns).
- **Call `cb(WatchErrorEvent{Err: err})` on errors.** Do not return from
  `Watch` for a recoverable error. Deliver that error through the callback, and
  continue the watch.
- **Return `nil` (or a meaningful error) when `ctx` is canceled.** A return of
  `ctx.Err()` is acceptable, because `kongfig.Watch` discards
  `context.Canceled`.

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

`providers/file` implements `WatchProvider` by default with
[fsnotify](https://github.com/fsnotify/fsnotify). A change to the file triggers
an automatic reload, and fires the `OnChange` callbacks.

The use is the same as the [Wiring up watch](#wiring-up-watch) section shows.
Pass the file provider to both `MustLoad` and `AddWatcher`:

```go
fp := fileprovider.New("config.yaml", yamlparser.Default)
kf.MustLoad(ctx, fp)
kf.AddWatcher(fp)
```

No extra setup is necessary. The watcher starts at the `kf.Watch(ctx)` call,
and it stops when the context is canceled.

---

## Derives and pipeline replay

`Kongfig.Derive` registers a compute step that runs after the load of one or
more providers. On a watch reload, kongfig replays the full **pipeline** from
the start. Every derived value therefore comes from the newest provider data.

Given this setup:

```go
k.MustLoad(ctx, providerA)  // step 1
k.MustLoad(ctx, providerB)  // step 2
k.Derive(func(in kongfig.DeriveInput) (kongfig.DeriveOutput, error) {
    // step 3: sees A merged with B
    return kongfig.DeriveOutput{Data: kongfig.ConfigData{
        "doubled": in.Data["value"].(int64) * 2,
    }}, nil
})
k.MustLoad(ctx, providerC)  // step 4

k.AddWatcher(providerA)
go k.Watch(ctx)
```

When `providerA` reloads, the pipeline replays in registration order:

1. Merge providerA's **new** data (step 1)
2. Merge providerB's stored snapshot (step 2)
3. **Re-run the derive** function against the merged A+B state (step 3)
4. Merge providerC's stored snapshot (step 4)

The derive at step 3 sees only the layers before it. The data of providerC is
invisible to the derive, because that provider comes later in the pipeline. The
initial load has the same semantics. Each step sees the accumulated state of
everything before it.

OnLoad hooks fire against the fully replayed proposed state. If any hook returns
an error, kongfig rejects the reload and keeps the earlier state.
