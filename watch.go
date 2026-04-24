package kongfig

import (
	"context"
	"log/slog"
)

type watchEntry struct {
	provider WatchProvider
	parser   Parser
	data     ProviderData
	source   string
}

// AddWatcher registers a WatchProvider to be started by Watch.
func (k *Kongfig) AddWatcher(wp WatchProvider) {
	e := watchEntry{provider: wp, source: wp.ProviderInfo().Name}
	if pp, ok := wp.(ParserProvider); ok {
		e.parser = pp.Parser()
	}
	if pds, ok := wp.(ProviderDataSupport); ok {
		e.data = pds.ProviderData()
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.hooks.watchers = append(k.hooks.watchers, e)
}

// OnChange registers a callback invoked whenever a watched provider fires.
func (k *Kongfig) OnChange(fn ChangeFunc) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.hooks.onChange = append(k.hooks.onChange, fn)
}

// reloadEntry builds LoadParsed options from w and calls LoadParsed.
func (k *Kongfig) reloadEntry(w watchEntry, data ConfigData) error {
	var lopts []LoadOption
	if w.parser != nil {
		lopts = append(lopts, WithParser(w.parser))
	}
	if w.data != nil {
		lopts = append(lopts, WithProviderData(w.data))
	}
	return k.LoadParsed(data, w.source, lopts...)
}

// fireOnChange copies the onChange slice under RLock and calls each handler.
func (k *Kongfig) fireOnChange() {
	k.mu.RLock()
	handlers := make([]ChangeFunc, len(k.hooks.onChange))
	copy(handlers, k.hooks.onChange)
	k.mu.RUnlock()
	for _, h := range handlers {
		h()
	}
}

// Watch starts all registered WatchProviders and blocks until ctx is canceled.
func (k *Kongfig) Watch(ctx context.Context) error {
	k.mu.RLock()
	watchers := make([]watchEntry, len(k.hooks.watchers))
	copy(watchers, k.hooks.watchers)
	k.mu.RUnlock()

	errc := make(chan error, len(watchers))

	for _, w := range watchers {
		go func() {
			errc <- w.provider.Watch(ctx, func(e WatchEvent) {
				switch ev := e.(type) {
				case WatchErrorEvent:
					k.log().Warn("watch provider error", slog.String("source", w.source), slog.Any("error", ev.Err))
				case WatchDataEvent:
					if reloadErr := k.reloadEntry(w, ev.Data); reloadErr != nil {
						k.log().Warn("watch reload failed", slog.String("source", w.source), slog.Any("error", reloadErr))
						return
					}
					k.fireOnChange()
				}
			})
		}()
	}

	<-ctx.Done()
	// Drain errors from watchers as they shut down.
	for range watchers {
		<-errc
	}
	return ctx.Err()
}
