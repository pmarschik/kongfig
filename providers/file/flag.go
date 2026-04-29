package file

import (
	"context"
	"fmt"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/schema"
)

// loadPathKey reads the value of key from kf's current merged state and loads
// the referenced file(s) as additional layers. The value may be a string (single
// path) or a []string / []any (multiple paths, e.g. after sep= splitting).
// No-op if the key is absent or all paths are empty.
func loadPathKey(ctx context.Context, kf *kongfig.Kongfig, key string, opts ...kongfig.LoadOption) (bool, error) {
	flat := kf.Flat()
	pathVal, ok := flat[key]
	if !ok {
		return false, nil
	}
	switch v := pathVal.(type) {
	case string:
		if v == "" {
			return false, nil
		}
		return true, loadOnePath(ctx, kf, key, v, opts...)
	case []string:
		return loadPathSlice(ctx, kf, key, v, opts...)
	case []any:
		paths := make([]string, 0, len(v))
		for _, elem := range v {
			if s, ok := elem.(string); ok {
				paths = append(paths, s)
			}
		}
		return loadPathSlice(ctx, kf, key, paths, opts...)
	default:
		return false, nil
	}
}

func loadOnePath(ctx context.Context, kf *kongfig.Kongfig, key, path string, opts ...kongfig.LoadOption) error {
	parser, err := kongfig.ParserForPath(path, kf.Parsers())
	if err != nil {
		return fmt.Errorf("file: config key %q = %q: %w", key, path, err)
	}
	return kf.Load(ctx, New(path, parser), opts...)
}

func loadPathSlice(ctx context.Context, kf *kongfig.Kongfig, key string, paths []string, opts ...kongfig.LoadOption) (bool, error) {
	loaded := false
	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := loadOnePath(ctx, kf, key, path, opts...); err != nil {
			return true, err
		}
		loaded = true
	}
	return loaded, nil
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
