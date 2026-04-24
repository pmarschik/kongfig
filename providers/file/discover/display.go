package discover

import (
	"context"
	"path/filepath"
	"strings"
)

type displayPathKey struct{}

// WithLongDisplayPaths returns a context that causes all [Discoverer.DisplayPath]
// implementations in this package to emit full, unabbreviated paths including the
// app subdirectory and filename.
//
// By default (without this option), discoverers return short symbolic tokens:
//
//	$xdg               (short, default)
//	$XDG_CONFIG_HOME/myapp/config.yaml   (long)
func WithLongDisplayPaths(ctx context.Context) context.Context {
	return context.WithValue(ctx, displayPathKey{}, true)
}

// displayPathIsLong reports whether long display paths are enabled in ctx.
func displayPathIsLong(ctx context.Context) bool {
	v, ok := ctx.Value(displayPathKey{}).(bool)
	return ok && v
}

// symPathContains reports whether foundPath is at or under base.
func symPathContains(base, foundPath string) bool {
	rel, err := filepath.Rel(base, foundPath)
	return err == nil && !strings.HasPrefix(rel, "..")
}

// symPath returns "token/rel" for the relative path of foundPath under base.
// Returns "" if foundPath is not under base. Callers should check [symPathContains] first.
func symPath(base, token, foundPath string) string {
	rel, err := filepath.Rel(base, foundPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	return token + "/" + rel
}
