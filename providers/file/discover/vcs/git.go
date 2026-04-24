package vcs

import (
	"context"
	"errors"

	gogit "github.com/go-git/go-git/v5"
)

// gitRootDiscoverer finds the config file at the git repository root using go-git.
// Unlike a simple .git dir-walk, go-git correctly resolves worktrees via commondir
// and follows .git files for submodules.
type gitRootDiscoverer struct{ opts }

// GitRoot returns a Discoverer that uses go-git to locate the git repository root
// and then searches for config.<ext> there.
//
// Handles git worktrees (linked worktrees use a .git file pointing to commondir),
// submodules (.git file pointing to parent repo's .git/modules/…), and bare repos.
// Returns ("", nil) if the start directory is not inside a git repository.
func GitRoot(options ...Option) *gitRootDiscoverer { //nolint:revive // concrete type for method chaining
	return &gitRootDiscoverer{resolveOpts(options)}
}

func (*gitRootDiscoverer) Name() string { return "git-root" }

func (d *gitRootDiscoverer) root() (string, error) {
	start, err := d.wd()
	if err != nil {
		return "", err
	}
	repo, err := gogit.PlainOpenWithOptions(start, &gogit.PlainOpenOptions{
		DetectDotGit:          true,
		EnableDotGitCommonDir: true,
	})
	if err != nil {
		if errors.Is(err, gogit.ErrRepositoryNotExists) {
			return "", nil // not a git repo — treat as "nothing found"
		}
		return "", err
	}
	wt, err := repo.Worktree()
	if err != nil {
		// bare repos have no worktree — treat as not found
		if errors.Is(err, gogit.ErrIsBareRepository) {
			return "", nil
		}
		return "", err
	}
	return wt.Filesystem.Root(), nil
}

func (d *gitRootDiscoverer) Discover(_ context.Context, exts []string) (string, error) {
	root, err := d.root()
	if err != nil || root == "" {
		return "", err
	}
	if p := findFile(root, "config", exts); p != "" {
		return p, nil
	}
	return "", nil
}

// DisplayPath returns the found path relative to the git repository root.
func (d *gitRootDiscoverer) DisplayPath(_ context.Context, foundPath string) string {
	root, err := d.root()
	if err != nil || root == "" {
		return ""
	}
	return relDisplay(root, "(git root)", foundPath)
}
