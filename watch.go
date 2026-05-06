package kongfig

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// WatchEvent is a sealed sum type representing a watch notification.
// Handle with a type switch:
//
//	switch ev := e.(type) {
//	case kongfig.WatchDataEvent:  // successful reload — ev.Data holds new config
//	case kongfig.WatchErrorEvent: // provider/reload error — ev.Err holds the error
//	}
type WatchEvent interface{ watchEvent() }

// WatchDataEvent carries the new config data after a successful reload.
type WatchDataEvent struct{ Data ConfigData }

// WatchErrorEvent carries an error from a watch provider or a reload failure.
type WatchErrorEvent struct{ Err error }

func (WatchDataEvent) watchEvent()  {}
func (WatchErrorEvent) watchEvent() {}

// WatchFunc is the callback type passed to WatchProvider.Watch.
// It is called whenever the watched source changes or encounters an error.
type WatchFunc func(WatchEvent)

// WatchProvider extends Provider with live-reload capability.
// The callback is invoked whenever the source changes.
// Watch must return when ctx is canceled.
type WatchProvider interface {
	Provider
	Watch(ctx context.Context, cb WatchFunc) error
}

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

// reloadEntry applies transforms to the new data, updates the matching pipeline entry,
// replays the full pipeline so derives re-run against the correct accumulated state,
// fires onLoad hooks, and commits if all hooks pass.
//
// If the provider is not found in the pipeline (e.g. AddWatcher called before Load),
// it falls back to LoadParsed to preserve the previous append-only behavior.
func (k *Kongfig) reloadEntry(w watchEntry, rawData ConfigData) error { //nolint:cyclop,funlen // pipeline replay: orchestrates normalize→rename→codec→find→replay→hook→commit in one function
	// Apply the same per-layer transforms that commitLayer applies to new provider data.
	data := normalizeConfigData(rawData)
	var renameWarnings []string
	var renameErr error
	data, renameWarnings, renameErr = k.applyRenames(data, w.source, "")
	if renameErr != nil {
		return renameErr
	}
	var err error
	data, err = applyBidirectionalCodecs(k, data)
	if err != nil {
		return err
	}

	var keyOrder map[string][]string
	if kop, ok := w.provider.(KeyOrderProvider); ok {
		if ko := kop.KeyOrder(); len(ko) > 0 {
			keyOrder = ko
		}
	}

	// Find the last pipeline entry for this source and update its snapshot.
	k.mu.Lock()
	pipelineIdx := -1
	for i := len(k.pipeline) - 1; i >= 0; i-- {
		if !k.pipeline[i].isDerive && k.pipeline[i].layer.Meta.Name == w.source {
			pipelineIdx = i
			break
		}
	}
	if pipelineIdx >= 0 {
		k.pipeline[pipelineIdx].layer.Data = data
		k.pipeline[pipelineIdx].layer.Meta.Timestamp = time.Now()
		if keyOrder != nil {
			k.pipeline[pipelineIdx].layer.KeyOrder = keyOrder
		}
	}
	oldData := k.data.Clone()
	k.mu.Unlock()

	// Fall back to append-only LoadParsed if provider is not in the pipeline.
	if pipelineIdx < 0 {
		var lopts []LoadOption
		if w.parser != nil {
			lopts = append(lopts, WithParser(w.parser))
		}
		if w.data != nil {
			lopts = append(lopts, WithProviderData(w.data))
		}
		if keyOrder != nil {
			lopts = append(lopts, withKeyOrder(keyOrder))
		}
		return k.LoadParsed(rawData, w.source, lopts...)
	}

	// Replay the full pipeline so derives re-run against the correct accumulated state.
	newData, newProv, newPipeline, err := k.replayPipeline()
	if err != nil {
		return err
	}

	// Fire onLoad hooks against the proposed state before committing.
	k.mu.RLock()
	reloadedLayer := k.pipeline[pipelineIdx].layer
	hooks := make([]LoadFunc, len(k.hooks.onLoad))
	copy(hooks, k.hooks.onLoad)
	k.mu.RUnlock()

	delta := pipelineStateDelta(oldData, newData)
	eventLayer := reloadedLayer
	eventLayer.Data = eventLayer.Data.Clone()
	event := LoadEvent{Layer: eventLayer, Delta: delta, ProposedData: newData}
	var errs []error
	for _, h := range hooks {
		if r := h(event); r.Err != nil {
			errs = append(errs, r.Err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	k.mu.Lock()
	k.data = newData
	k.prov = newProv
	k.pipeline = newPipeline
	k.cfg.migrationWarnings = append(k.cfg.migrationWarnings, renameWarnings...)
	k.mu.Unlock()

	return nil
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
