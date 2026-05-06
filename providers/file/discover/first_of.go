package discover

import (
	"context"
	"sync"
)

// firstOfDiscoverer tries sub-discoverers in order and returns the first non-empty result.
//
//nolint:govet // fieldalignment: mu before winner groups non-pointer fields; intentional
type firstOfDiscoverer struct {
	mu          sync.Mutex
	winner      innerDiscoverer // set by the most recent Discover call that found a file
	discoverers []innerDiscoverer
}

// FirstOf returns a Discoverer that tries each sub-discoverer in order and
// returns the first non-empty result, short-circuiting the rest.
//
// Name() and DisplayPath() delegate to the winning discoverer after Discover
// is called. If no discoverer finds a file, Name() returns the first
// sub-discoverer's name (so the empty provider's source label is meaningful).
//
// This is the discoverer-level analog of [LocateFirst]:
//
//	// Try env-injected path first, then XDG, then workdir.
//	d := discover.FirstOf(envOverride, discover.XDG(), discover.Workdir())
//	p, err := file.Discover(ctx, d, yaml.Default)
func FirstOf(discoverers ...innerDiscoverer) *firstOfDiscoverer {
	return &firstOfDiscoverer{discoverers: discoverers}
}

// Name returns the winning discoverer's name after a successful Discover call,
// or the first sub-discoverer's name if none have fired yet.
func (f *firstOfDiscoverer) Name() string {
	f.mu.Lock()
	w := f.winner
	f.mu.Unlock()
	if w != nil {
		return w.Name()
	}
	if len(f.discoverers) > 0 {
		return f.discoverers[0].Name()
	}
	return "first-of"
}

// Discover tries each sub-discoverer in order and returns the first non-empty result.
// Updates the internal winner so Name() and DisplayPath() reflect which stage fired.
func (f *firstOfDiscoverer) Discover(ctx context.Context, exts []string) (string, error) {
	f.mu.Lock()
	f.winner = nil
	f.mu.Unlock()

	for _, d := range f.discoverers {
		path, err := d.Discover(ctx, exts)
		if err != nil {
			return "", err
		}
		if path != "" {
			f.mu.Lock()
			f.winner = d
			f.mu.Unlock()
			return path, nil
		}
	}
	return "", nil
}

// DisplayPath delegates to the winning discoverer's DisplayPath if it implements
// the optional interface; returns "" otherwise.
func (f *firstOfDiscoverer) DisplayPath(ctx context.Context, foundPath string) string {
	f.mu.Lock()
	w := f.winner
	f.mu.Unlock()
	if w == nil {
		return ""
	}
	type displayPather interface {
		DisplayPath(context.Context, string) string
	}
	if dp, ok := w.(displayPather); ok {
		return dp.DisplayPath(ctx, foundPath)
	}
	return ""
}
