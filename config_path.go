package kongfig

import "github.com/pmarschik/kongfig/schema"

// ConfigPathEntry is an alias for [schema.ConfigPathEntry].
// Prefer this import path in application code; importing the schema sub-package
// directly is not necessary for typical usage.
type ConfigPathEntry = schema.ConfigPathEntry

// withConfigPaths returns an internal Option that stores config path entries on Kongfig.
func withConfigPaths(entries []ConfigPathEntry) Option {
	return func(k *Kongfig) { k.cfg.configPaths = entries }
}

// ConfigPaths returns the config path entries registered by [NewFor] from
// kongfig-path struct tags. Returns nil if none are registered.
// Pass the result to fileprovider.MustLoadConfigPaths to load the referenced files.
func (k *Kongfig) ConfigPaths() []ConfigPathEntry {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if len(k.cfg.configPaths) == 0 {
		return nil
	}
	out := make([]ConfigPathEntry, len(k.cfg.configPaths))
	copy(out, k.cfg.configPaths)
	return out
}
