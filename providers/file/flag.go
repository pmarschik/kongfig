package file

import (
	"context"
	"fmt"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/schema"
)

// loadPathKey reads the value of key from kf's current merged state as a string
// path and loads that file as an additional layer. No-op if the key is absent or empty.
func loadPathKey(ctx context.Context, kf *kongfig.Kongfig, key string, opts ...kongfig.LoadOption) (bool, error) {
	flat := kf.Flat()
	pathVal, ok := flat[key]
	if !ok {
		return false, nil
	}
	path, ok2 := pathVal.(string)
	if !ok2 || path == "" {
		return false, nil
	}
	parser, err := kongfig.ParserForPath(path, kf.Parsers())
	if err != nil {
		return true, fmt.Errorf("file: config key %q = %q: %w", key, path, err)
	}
	return true, kf.Load(ctx, New(path, parser), opts...)
}

// LoadConfigPaths loads files for each entry in entries by reading the path value
// from kf's current merged state (via the entry's Key) and loading the file.
// Entries are processed in the order given; use [kongfig.Kongfig.ConfigPaths] or
// [kongfig.ConfigPaths] to obtain a pre-sorted slice.
//
// Returns on the first error. Files whose key is absent or empty are silently skipped.
func LoadConfigPaths(ctx context.Context, kf *kongfig.Kongfig, entries []schema.ConfigPathEntry, opts ...kongfig.LoadOption) error {
	for _, e := range entries {
		if _, err := loadPathKey(ctx, kf, e.Key, opts...); err != nil {
			return err
		}
	}
	return nil
}

// MustLoadConfigPaths is like [LoadConfigPaths] but panics on error.
func MustLoadConfigPaths(ctx context.Context, kf *kongfig.Kongfig, entries []schema.ConfigPathEntry, opts ...kongfig.LoadOption) {
	if err := LoadConfigPaths(ctx, kf, entries, opts...); err != nil {
		panic(err)
	}
}
