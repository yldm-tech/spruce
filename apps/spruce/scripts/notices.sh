#!/usr/bin/env bash
# notices.sh writes resources/THIRD_PARTY_NOTICES.txt, which every package nsgo build makes carries (nsgo copies the resources folder into it): the license, notice and patent files of Go and of every third-party module linked into the app for macOS and Windows on arm64 and amd64 (go list -deps without cgo), together with those of the folders inside a module that hold its linked packages, such as NSGo's internal/shader. The MIT and BSD licenses ask for their text to ship with the binaries, and Apache-2.0 for the NOTICE files. With --check it writes nothing and fails when the file is not what it would write, which scripts/check.sh runs after a dependency changes.
set -euo pipefail
cd "$(dirname "$0")/.."

out=resources/THIRD_PARTY_NOTICES.txt
check=false
case ${1:-} in
  --check) check=true ;;
  "") ;;
  *)
    echo "usage: scripts/notices.sh [--check]" >&2
    exit 2
    ;;
esac

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Each linked package of a module that is not the app's: module path, version, module folder, package folder.
for t in darwin/arm64 darwin/amd64 windows/amd64 windows/arm64; do
  GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go list -deps -f '{{if .Module}}{{if not .Module.Main}}{{.Module.Path}}	{{.Module.Version}}	{{.Module.Dir}}	{{.Dir}}{{end}}{{end}}' . >>"$work/packages"
done
LC_ALL=C sort -u "$work/packages" -o "$work/packages"

# license_files <folder> prints the license, notice and patent files directly inside the folder.
license_files() {
  find "$1" -maxdepth 1 -type f \( -iname 'LICENSE*' -o -iname 'LICENCE*' -o -iname 'NOTICE*' -o -iname 'COPYING*' -o -iname 'PATENTS*' \) | LC_ALL=C sort
}

# module_files <module path> <module folder> prints the license files of the module's root and of the folders between each of its linked packages and the root, for licenses that cover only part of a module.
module_files() {
  local path=$1 dir=$2 pkg
  {
    license_files "$dir"
    awk -F '\t' -v path="$path" '$1 == path { print $4 }' "$work/packages" | while read -r pkg; do
      while [ "$pkg" != "$dir" ] && [ "${pkg#"$dir"/}" != "$pkg" ]; do
        license_files "$pkg"
        pkg=$(dirname "$pkg")
      done
    done
  } | LC_ALL=C sort -u
}

rule='================================================================================'

# section <title> <root> <file>... prints a section with each file's path relative to root and its text.
section() {
  local title=$1 root=$2 file
  shift 2
  printf '\n%s\n%s\n%s\n' "$rule" "$title" "$rule"
  for file in "$@"; do
    printf '\n--- %s ---\n\n' "${file#"$root"/}"
    cat "$file"
    # A file whose last line has no newline would run into the next heading.
    if [ -n "$(tail -c 1 "$file")" ]; then
      echo
    fi
  done
}

{
  echo "Third-party notices for Spruce"
  echo
  echo "Spruce itself is under the MIT License (LICENSE in https://github.com/yldm-tech/spruce). Its executables also contain the code below, each part under the license that follows its name. The bundled Inter fonts are under the SIL Open Font License 1.1, in fonts/Inter-LICENSE.txt beside this file."
  echo
  echo "apps/spruce/scripts/notices.sh writes this file from the modules the app links on macOS and Windows; do not edit it by hand."

  goroot=$(go env GOROOT)
  files=()
  while read -r file; do
    files+=("$file")
  done < <(license_files "$goroot")
  section "The Go programming language (runtime and standard library)" "$goroot" "${files[@]}"

  while IFS=$'\t' read -r path version dir; do
    files=()
    while read -r file; do
      files+=("$file")
    done < <(module_files "$path" "$dir")
    if [ "${#files[@]}" -eq 0 ]; then
      echo "notices: $path $version has no license file" >&2
      exit 1
    fi
    section "$path $version" "$dir" "${files[@]}"
  done < <(cut -f 1-3 "$work/packages" | LC_ALL=C sort -u)
} >"$work/notices"

if [ "$check" = true ]; then
  if ! cmp -s "$work/notices" "$out"; then
    echo "notices: $out is out of date with the linked modules; run apps/spruce/scripts/notices.sh and commit it" >&2
    exit 1
  fi
  exit 0
fi
cat "$work/notices" >"$out"
