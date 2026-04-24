package discover

import (
	"context"
	"sync"

	kongfig "github.com/pmarschik/kongfig"
)

// innerDiscoverer is the subset of file.Discoverer used by this package.
// Defined locally to avoid a circular import with the file package.
type innerDiscoverer interface {
	Name() string
	Discover(context.Context, []string) (string, error)
}

// deprecatedDiscoverer wraps an innerDiscoverer and fires a [kongfig.LegacyFileEvent]
// whenever the inner discoverer finds a file.
type deprecatedDiscoverer struct {
	inner         innerDiscoverer
	policy        kongfig.MigrationPolicy
	preferredPath string
	seen          int
	mu            sync.Mutex
}

// Deprecated wraps d so that whenever it finds a file, a [kongfig.LegacyFileEvent]
// is dispatched through policy (default: [kongfig.DefaultMigrationPolicy]).
//
// Use this to phase out legacy config file locations: wrap the old discoverer with
// Deprecated and add the new location unwrapped.
//
// Example:
//
//	// Old XDG path is deprecated; preferred location is the workdir.
//	file.Discover(ctx,
//	    discover.Deprecated(discover.XDG(), "~/.config/myapp/config.yaml"),
//	    yaml.Default)
func Deprecated(d innerDiscoverer, preferredPath string, policy ...kongfig.MigrationPolicy) *deprecatedDiscoverer { //nolint:revive // returning concrete type allows callers to chain methods
	p := kongfig.DefaultMigrationPolicy
	if len(policy) > 0 {
		p = policy[0]
	}
	return &deprecatedDiscoverer{inner: d, preferredPath: preferredPath, policy: p}
}

func (d *deprecatedDiscoverer) Name() string { return d.inner.Name() }

func (d *deprecatedDiscoverer) Discover(ctx context.Context, exts []string) (string, error) {
	path, err := d.inner.Discover(ctx, exts)
	if err != nil || path == "" {
		return path, err
	}

	d.mu.Lock()
	d.seen++
	occ := d.seen
	d.mu.Unlock()

	event := kongfig.LegacyFileEvent{
		FilePath:      path,
		PreferredPath: d.preferredPath,
		SourceName:    d.inner.Name(),
		Occurrence:    occ,
	}

	h := d.policy.OnRepeat
	if occ == 1 {
		h = d.policy.OnFirst
	}
	if h != nil {
		if hErr := h(event); hErr != nil {
			return "", hErr
		}
	}

	return path, nil
}

// DisplayPath forwards to the inner discoverer if it supports the optional interface.
func (d *deprecatedDiscoverer) DisplayPath(ctx context.Context, foundPath string) string {
	type displayPather interface {
		DisplayPath(context.Context, string) string
	}
	if dp, ok := d.inner.(displayPather); ok {
		return dp.DisplayPath(ctx, foundPath)
	}
	return ""
}
