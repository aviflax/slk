#!/bin/sh
# Print the body of one CHANGELOG.md section, for use as release notes.
#
#   scripts/changelog-section.sh <version> [changelog]
#
# <version> may carry a leading "v" (a tag name works). The section runs from
# its "## [<version>]" heading to the next "## [" heading or the link
# references at the end of the file. Exits non-zero, printing nothing to
# stdout, when the section is missing or empty, so a release cannot go out
# without a changelog entry.
set -eu

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
  echo "usage: $0 <version> [changelog]" >&2
  exit 2
fi

version=${1#v}
changelog=${2:-CHANGELOG.md}

if [ ! -f "$changelog" ]; then
  echo "$changelog not found" >&2
  exit 1
fi

section=$(awk -v ver="$version" '
  /^## \[/ {
    if (found) exit
    if (index($0, "## [" ver "]") == 1) { found = 1; next }
  }
  found && /^\[[^]]+\]: / { exit }
  found { print }
' "$changelog")

# Trim blank lines at both ends, then require something left.
section=$(printf '%s\n' "$section" | awk 'NF { p = 1 } p { print }' | awk '{ a[NR] = $0 } END { n = NR; while (n > 0 && a[n] == "") n--; for (i = 1; i <= n; i++) print a[i] }')

if [ -z "$section" ]; then
  echo "no CHANGELOG.md entry for version $version" >&2
  exit 1
fi

printf '%s\n' "$section"
