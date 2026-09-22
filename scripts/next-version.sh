#!/usr/bin/env bash
# Print the next ChairLift calendar version tag.
#
# Scheme: vYY.MM.N[-PRERELEASE]
#
#   YY  two-digit year, MM zero-padded month — the release's calendar slot,
#       matching how the Bluefin images are dated (stable-YYYYMMDD).
#   N   sequence within that month, starting at 0.
#
# The leading zero in MM is deliberate and is why this is not svu: it reads
# as a date. GoReleaser's semver parser accepts it and normalises 26.09.0 to
# 26.9.0 internally, so `{{ .Version }}` renders without the zero while the
# tag and the About dialog keep it. That is why the release build injects
# `{{ .Tag }}`, not `{{ .Version }}`.
#
# Usage:
#   scripts/next-version.sh            -> v26.09.0   (or v26.09.1 if 0 exists)
#   scripts/next-version.sh alpha.1    -> v26.09.0-alpha.1
set -euo pipefail

prerelease="${1:-}"

slot="$(date +%y.%m)"

# Highest N already tagged in this calendar slot. Release and prerelease tags
# share the sequence, so an alpha does not silently reuse a released number.
highest="$(
	git tag --list "v${slot}.*" |
		sed -E "s/^v${slot}\.([0-9]+).*$/\1/" |
		grep -E '^[0-9]+$' |
		sort -n |
		tail -1 ||
		true
)"

if [ -z "${highest}" ]; then
	next=0
else
	next=$((highest + 1))
fi

version="v${slot}.${next}"
if [ -n "${prerelease}" ]; then
	version="${version}-${prerelease}"
fi

printf '%s\n' "${version}"
