package vcs

import (
	"context"
	"os/exec"
	"strings"
)

// jjRootDiscoverer finds the config file at the Jujutsu workspace root by
// forking `jj root`. This handles all jj workspace configurations including
// colocated repos (where both .jj/ and .git/ exist) and multi-workspace setups.
type jjRootDiscoverer struct{ opts }

// JujutsuRoot returns a Discoverer that forks `jj root` to locate the Jujutsu workspace
// root and then searches for config.<ext> there.
//
// Returns ("", nil) if jj is not installed or the directory is not inside a jj workspace.
func JujutsuRoot(options ...Option) *jjRootDiscoverer { //nolint:revive // concrete type for method chaining
	return &jjRootDiscoverer{resolveOpts(options)}
}

func (*jjRootDiscoverer) Name() string { return "jj-root" }

func (d *jjRootDiscoverer) root(ctx context.Context) (string, error) {
	start, err := d.wd()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "jj", "root")
	cmd.Dir = start
	out, err := cmd.Output()
	if err != nil {
		return "", nil //nolint:nilerr // any jj error means "not a jj workspace or jj not installed"
	}
	return strings.TrimSpace(string(out)), nil
}

func (d *jjRootDiscoverer) Discover(ctx context.Context, exts []string) (string, error) {
	root, err := d.root(ctx)
	if err != nil || root == "" {
		return "", err
	}
	if p := findFile(root, "config", exts); p != "" {
		return p, nil
	}
	return "", nil
}

// DisplayPath returns the found path relative to the Jujutsu workspace root.
func (d *jjRootDiscoverer) DisplayPath(ctx context.Context, foundPath string) string {
	root, err := d.root(ctx)
	if err != nil || root == "" {
		return ""
	}
	return relDisplay(root, "(jj root)", foundPath)
}
