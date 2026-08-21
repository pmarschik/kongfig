<!-- read_when: cutting a release, changing the release tasks, or recovering a half-finished release -->

# Releasing kongfig

kongfig publishes libraries, not binaries. A release is done when the tags reach
GitHub and the module proxy serves them. Every workspace module gets its own
tag, so one release writes many tags onto one commit.

The tasks are shared with the other Go repos, so they live in
[devkit](https://github.com/pmarschik/devkit) under `mise/go-release/`, not in
this repo. `.config/mise/config.toml` pulls them in:

```toml
[task_config]
includes = [
  "git::https://github.com/pmarschik/devkit.git//mise/go-release?ref=v1",
  ".config/mise/tasks",
]
```

Fix a task in devkit, then move its `v1` tag and clear the cache here. Read
"Changing a task" below for the cache step. To shadow one task without touching
devkit, drop a file of the same name into `.config/mise/tasks/release/` — the
local include is last, so it wins.

This repo supplies what the tasks read: `go.work`, `.config/cliff.toml` with
`tag_pattern = "^v[0-9]"`, and `CHANGELOG.md`.

| Task                | What it does                                                         |
| ------------------- | -------------------------------------------------------------------- |
| `release:prepare`   | Writes the changelog, pins the module versions, describes the commit |
| `release:push`      | Verifies the prepared release, tags it, pushes it, tidies `go.sum`   |
| `release:rollback`  | Undoes a local `release:prepare` that you have not pushed            |
| `release:post-tidy` | Redoes the `go.sum` step when the proxy was still catching up        |

## Before you start

- Run `mise run check`. All gates must pass.
- Land every commit you want in the release.
- Start from a clean working copy. `release:prepare` refuses a dirty `@`.

## Step 1 — prepare

```bash
mise run release:prepare
mise run release:prepare --highlights "One line that opens the release notes"
```

The task does this:

1. Checks the commits with `cog check`.
2. Asks `git cliff` for the next version, and rejects anything that is not
   `vX.Y.Z`.
3. Writes `CHANGELOG.md` and formats it with `hk fix`.
4. Pins every intra-repo `require` to the new version and drops intra-repo
   `replace` directives.
5. Pins the same version in `go.work`, one `replace` per intra-repo module.
6. Under jj: describes `@` as `chore(release): vX.Y.Z` and moves the `main`
   bookmark onto it.

Flags: `--force` skips every pre-flight check, `--skip-clean` skips the dirty
check, and `--skip-cog` skips the commit check.

Now read the diff. `CHANGELOG.md`, `go.work` and the `go.mod` files are the
whole change.

Step 5 keeps the repo buildable between prepare and push. The modules require
each other in both directions, so the root module needs a `replace` as much as
its submodules do. Without those lines every build fails with `unknown revision
vX.Y.Z`, because the proxy has nothing to serve until the tags land.

The release commit stays at `@`, untagged and mutable, so `jj diff` shows the
change and you can amend it in place. Run `release:prepare` again after an
amend, and it re-prepares the same version rather than bumping past it.

`release:push` creates the tags. A tag freezes its commit under jj, which would
slide your working copy onto an empty child and leave `jj diff` empty in the
middle of your review.

Under git the task stops after step 5. The commit and the tags happen in
`release:push`, where an unwanted commit costs less to undo.

## Step 2 — push

```bash
mise run release:push --dry-run   # verify and report, reach no remote
mise run release:push
```

The dry run checks that `main` sits on the prepared commit. It then prints the
tags it would create and push and stops. It changes nothing, locally or
remotely.

The real run asks for confirmation, then:

1. Verifies the prepared jj state again, creates the release tags, and leaves an
   empty child of the release commit.
2. Pushes the `main` bookmark. **This needs a hardware key touch.**
3. Pushes the root tag on its own. GitHub drops push events when more than
   three tags arrive together, and `.github/workflows/release.yml` waits for
   this one tag.
4. Pushes the module tags.
5. Opens or updates the GitHub release from the changelog section.
6. Waits up to 120 seconds for the proxy to serve the root module.
7. Runs `go work sync` and `go mod tidy` in every module, commits the result as
   `chore: update go.sum after vX.Y.Z`, and pushes it. **Second key touch.**

The tags stay on the release commit. Only the branch moves on to the `go.sum`
commit.

Step 7 needs `GOWORK=off`. Inside the workspace every intra-repo dependency
resolves to a local directory and never earns a checksum.

## Recovering

**The proxy timed out.** `release:push` says so and exits after step 6. Run
`mise run release:post-tidy` once the proxy answers, then commit the `go.sum`
changes yourself.

**You want the prepared release back.** Run `mise run release:rollback` before
you push. It moves `main` back one commit, restores the release files, and
clears the description. After a push that died between tagging and the remote,
it also deletes those tags, and it finds the release at `@-` because the tags
froze it.

**`release:prepare` died partway.** The half-written files sit in `@` with no
description, so `release:rollback` has nothing to recognise. Run `jj restore` on
the release files, then prepare again.

**The GitHub Actions run never fired.** GitHub creates no tag event when more
than three tags arrive in one push, and a release writes one tag per module onto
one commit. `release:push` pushes the root tag on its own for that reason, so a
plain `jj git push` around the task — which carries the branch and every tag
together — silently costs the run. Check with `jj op log`: one
`push bookmark main, tags …` entry means the event never happened.

The Actions tab has nothing to re-run in that case, because no run exists.
Dispatch one instead:

```bash
gh workflow run release.yml -f tag=vX.Y.Z
```

The dispatch reads the workflow from the default branch, so it also covers a tag
whose own commit predates the `workflow_dispatch` trigger. It checks out the tag
and passes it as `GORELEASER_CURRENT_TAG`.

A missed run costs nothing on its own: `.config/goreleaser.yaml` skips builds,
and `release:push` writes the release notes itself.

## Adding a module

`discover_modules` reads `go.work`, so a new module joins the release as soon as
you add it to the workspace. Directories that match `example/*` stay out. Add
the matching scope to `cog.toml` in the same change.

## Changing a task

Edit it in devkit, run `mise run check` there, then move the `v1` tag onto the
new commit.

The moved tag does not reach this repo on its own. mise clones the include once
and pins that clone to the commit the ref named at the time, so the old task
keeps running with a clean exit and no warning. Clear the cache here:

```bash
rm -rf "$(mise cache path)/remote-git-tasks-cache"
mise tasks ls
```

`MISE_TASK_REMOTE_NO_CACHE=1` refetches for one invocation only and leaves the
cache untouched. Use it to try a change out, and delete the directory to make
the change stick.
