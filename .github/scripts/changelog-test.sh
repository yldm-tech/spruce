#!/usr/bin/env bash
# changelog-test.sh
#
# Checks changelog.sh on a repository of its own, made in a temporary folder: the section add writes from the commits since the previous version tag (pull requests by their titles, scopes that are not the app left out, reverts settled), that it puts a section above those of older versions and keeps one already there, the line it writes for a version with nothing to list, that notes prints a section the way the release reads it, and that next-version.sh releases nothing for the website scope, which changelog.sh leaves out too, while a dependency bump releases.
set -euo pipefail

scripts=$(cd "$(dirname "$0")" && pwd)
script=$scripts/changelog.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$work"

git init -q
git config user.name test
git config user.email test@example.com
git config commit.gpgsign false
git config tag.gpgsign false
mkdir -p apps/spruce

# commit <subject> [body] makes an empty commit dated 2026-01-02, the date the sections take from the commit they are written at.
commit() {
  GIT_AUTHOR_DATE=2026-01-02T00:00:00Z GIT_COMMITTER_DATE=2026-01-02T00:00:00Z git commit -q --allow-empty -m "$1" ${2:+-m "$2"}
}

fail() {
  echo "changelog-test: $1" >&2
  exit 1
}

# expect <name> <want> <got>
expect() {
  if [ "$2" != "$3" ]; then
    printf 'changelog-test: %s\n--- want\n%s\n--- got\n%s\n' "$1" "$2" "$3" >&2
    exit 1
  fi
}

# merge <branch> <message...> merges the branch into the current one in a merge commit with the date commit gives, and the message's paragraphs, as GitHub writes a pull request's merge with its title in the body.
merge() {
  local branch=$1 args=() m
  shift
  for m in "$@"; do
    args+=(-m "$m")
  done
  GIT_AUTHOR_DATE=2026-01-02T00:00:00Z GIT_COMMITTER_DATE=2026-01-02T00:00:00Z git merge -q --no-ff "${args[@]}" "$branch"
}

# branch <name> <subject...> makes a branch off the current commit with a commit for each subject, and goes back.
branch() {
  local name=$1 subject
  shift
  git checkout -q -b "$name"
  for subject in "$@"; do
    commit "$subject"
  done
  git checkout -q -
}

commit "feat: first"
commit "feat(films): add a filter"
git tag -a v0.1.0 -m v0.1.0
commit "fix(player): stop on the last frame"
commit "docs: explain the queue"
commit "feat(films): add a sort menu"
commit "refactor: split the view"
commit "perf: draw fewer frames"
commit "feat(shelf)!: move the library"
commit "fix: keep the cue" "BREAKING CHANGE: cues are stored apart"
commit "revert: feat(films): add a sort menu"
commit "revert: feat(films): add a filter"
commit "not a conventional commit"
commit "fix: stop on the last frame"
commit "feat(site): redesign the landing page"
commit "feat(site)!: move the landing page to its own domain"
commit "fix(deps): bump a library"
commit "fix(deps): keep the window across reloads"
branch side "fix: from a merged branch"
merge side "Merge branch side"
branch volume "feat(player): add volume control" "fix(player): address review findings" "feat(media)!: drop the old cache"
merge volume "Merge pull request #2 from example/volume" "feat(player): add volume control and shortcuts"
branch icon "feat(site): rework the page" "feat(brand): replace the app icon"
merge icon "Merge pull request #3 from example/icon" "feat(site): rework the page and the app icon"

"$script" add v0.2.0
want=$(
  cat <<'END'
# Changelog

What changed in each version of Spruce, newest first. Each section is also the notes of the GitHub release of that version.

## 0.2.0 - 2026-01-02

### Breaking changes

- Move the library
- Keep the cue
- Drop the old cache

### Features

- Add volume control and shortcuts
- Replace the app icon

### Fixes

- Stop on the last frame
- Revert "add a filter"
- From a merged branch

### Performance

- Draw fewer frames
END
)
expect "first section" "$want" "$(cat apps/spruce/CHANGELOG.md)"

git tag -a v0.2.0 -m v0.2.0
commit "chore: tidy"
"$script" add v0.2.1
expect "newest first" "## 0.2.1 - 2026-01-02 ## 0.2.0 - 2026-01-02" "$(grep '^## ' apps/spruce/CHANGELOG.md | paste -sd ' ' -)"
expect "nothing to list" "No changes to the app worth listing: this version only carries maintenance." "$("$script" notes v0.2.1)"

# A section written ahead for a later version stays above the one add writes.
awk '/^## / && !done { print "## 0.4.0"; print ""; print "Written ahead."; print ""; done = 1 } { print }' apps/spruce/CHANGELOG.md >changelog.tmp
mv changelog.tmp apps/spruce/CHANGELOG.md
commit "fix: keep the order"
"$script" add v0.2.2
expect "older versions below" "## 0.4.0 ## 0.2.2 - 2026-01-02 ## 0.2.1 - 2026-01-02 ## 0.2.0 - 2026-01-02" "$(grep '^## ' apps/spruce/CHANGELOG.md | paste -sd ' ' -)"
expect "section between" "### Fixes

- Keep the order" "$("$script" notes v0.2.2)"

before=$(cat apps/spruce/CHANGELOG.md)
"$script" add v0.2.0 2>/dev/null
expect "a section already there is kept" "$before" "$(cat apps/spruce/CHANGELOG.md)"

# A section may be written by hand, with the heading Keep a Changelog uses, and is what notes prints.
printf '# Changelog\n\n## [0.3.0] - 2026-02-03\n\n\nWritten by hand.\n\n## 0.2.1\n\nOlder.\n' >apps/spruce/CHANGELOG.md
"$script" add v0.3.0 2>/dev/null
expect "hand-written notes" "Written by hand." "$("$script" notes 0.3.0)"

printf '# Changelog\n\n## 0.3.0\n\n## 0.2.1\n\nOlder.\n' >apps/spruce/CHANGELOG.md
if "$script" add v0.3.0 2>/dev/null; then
  fail "add accepted an empty section"
fi
if "$script" notes v0.3.0 2>/dev/null; then
  fail "notes printed an empty section"
fi
if "$script" notes v9.0.0 2>/dev/null; then
  fail "notes printed a version that has no section"
fi
if "$script" add 1.2 2>/dev/null; then
  fail "add accepted 1.2 as a version"
fi

# next-version.sh, on a repository of its own: the website's scope releases nothing, breaking or not, while a dependency bump and a change to the app do. Each scope it skips is one changelog.sh leaves out of the notes.
skipped_notes=$(sed -nE 's/^skip_scopes="(.*)"$/\1/p' "$scripts/changelog.sh")
skipped_release=$(sed -nE 's/^skip_scopes="(.*)"$/\1/p' "$scripts/next-version.sh")
if [ -z "$skipped_release" ]; then
  fail "next-version.sh has no skip_scopes"
fi
for scope in $skipped_release; do
  if [[ " $skipped_notes " != *" $scope "* ]]; then
    fail "next-version.sh skips the scope $scope, which changelog.sh lists in the notes"
  fi
done
mkdir "$work/next"
cd "$work/next"
git init -q
git config user.name test
git config user.email test@example.com
git config commit.gpgsign false
git config tag.gpgsign false
commit "feat: first"
git tag -a v0.1.0 -m v0.1.0
commit "feat(site): redesign the landing page"
commit "feat(site)!: move the landing page to its own domain"
commit "fix(site): fix a link" "BREAKING CHANGE: the old address is gone"
branch landing "feat(site): add a download section" "docs: explain the site"
merge landing "Merge pull request #4 from example/landing" "feat(site): add a download section"
expect "website only" "" "$("$scripts/next-version.sh" HEAD)"
commit "fix(deps): bump a library"
expect "a dependency bump" "v0.1.1" "$("$scripts/next-version.sh" HEAD)"
commit "fix(cleanup): keep the selection"
expect "a fix to the app" "v0.1.1" "$("$scripts/next-version.sh" HEAD)"
commit "feat(deps)!: drop an old library"
expect "a breaking dependency bump" "v0.2.0" "$("$scripts/next-version.sh" HEAD)"
commit "feat: add a filter"
expect "a feature of the app" "v0.2.0" "$("$scripts/next-version.sh" HEAD)"
commit "feat(space)!: rework the treemap"
expect "a breaking change to the app" "v0.2.0" "$("$scripts/next-version.sh" HEAD)"
expect "a forced bump" "v1.0.0" "$("$scripts/next-version.sh" HEAD major)"

echo "changelog-test: ok"
