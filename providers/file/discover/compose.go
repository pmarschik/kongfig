package discover

import "context"

// compositeDiscoverer combines a DirProvider and a FileLocator into a Discoverer.
type compositeDiscoverer struct {
	dirs   DirProvider
	locate FileLocator
	name   string
}

// Compose creates a compositeDiscoverer with the given name, dir provider, and file locator.
func Compose(name string, dirs DirProvider, locate FileLocator) *compositeDiscoverer { //nolint:revive // returning concrete type allows callers to chain methods
	return &compositeDiscoverer{name: name, dirs: dirs, locate: locate}
}

// ComposeAll creates one compositeDiscoverer per locator, sharing the same name and dir provider.
func ComposeAll(baseName string, dirs DirProvider, locs ...FileLocator) []*compositeDiscoverer { //nolint:revive // returning concrete type allows callers to chain methods
	out := make([]*compositeDiscoverer, len(locs))
	for i, loc := range locs {
		out[i] = Compose(baseName, dirs, loc)
	}
	return out
}

// Name returns the discoverer name.
func (c *compositeDiscoverer) Name() string { return c.name }

// Discover searches for a config file using the dir provider and file locator.
func (c *compositeDiscoverer) Discover(ctx context.Context, exts []string) (string, error) {
	entries, err := c.dirs(ctx)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if p := c.locate(ctx, entry.Path, exts); p != "" {
			return p, nil
		}
	}
	return "", nil
}

// DisplayPath returns a human-friendly display path for the found file.
func (c *compositeDiscoverer) DisplayPath(ctx context.Context, foundPath string) string {
	entries, err := c.dirs(ctx)
	if err != nil {
		return ""
	}
	return displayPathFromEntries(ctx, entries, foundPath)
}

// displayPathFromEntries returns a human-friendly display path for foundPath
// based on the given DirEntries. Short mode uses entry.Short; long mode uses entry.Long.
func displayPathFromEntries(ctx context.Context, entries []DirEntry, foundPath string) string {
	long := DisplayPathIsLong(ctx)
	for _, entry := range entries {
		if symPathContains(entry.Path, foundPath) {
			if long {
				return symPath(entry.Path, entry.Long, foundPath)
			}
			return entry.Short
		}
	}
	return ""
}
