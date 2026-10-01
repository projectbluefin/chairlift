#!/usr/bin/env bash
# Print the next ChairLift calendar version tag.
#
# Scheme: vYY.MM.N
#
#   YY  two-digit year, MM zero-padded month — the release's calendar slot,
#       matching how the Bluefin images are dated (stable-YYYYMMDD).
#   N   stable point-release sequence within that month, starting at 1.
#
# The leading zero in MM is deliberate and is why this is not svu: it reads
# as a date. GoReleaser's semver parser accepts it and normalises 26.09.0 to
# 26.9.0 internally, so `{{ .Version }}` renders without the zero while the
# tag and the About dialog keep it. That is why the release build injects
# `{{ .Tag }}`, not `{{ .Version }}`.
#
# Usage:
#   scripts/next-version.sh            -> v26.10.1   (or v26.10.2 if 1 exists)
set -euo pipefail

if [ "$#" -ne 0 ]; then
	echo "next-version: stable releases only; no arguments accepted" >&2
	exit 1
fi

# NEXT_VERSION_SLOT pins the calendar slot (YY.MM); tests use it so the
# answer does not depend on today's date.
slot="${NEXT_VERSION_SLOT:-$(date +%y.%m)}"

# The slot is a regular-expression fragment in the sed below, so its dot must
# be literal: unescaped, `26.09` also matches `26x09`.
slot_pattern="${slot//./\\.}"

# Only stable tags advance the monthly sequence; historical alpha tags remain
# available but never influence a new stable release.
highest="$(
	git tag --list "v${slot}.*" |
		grep -E "^v${slot_pattern}\.[0-9]+$" |
		sed -E "s/^v${slot_pattern}\.([0-9]+)$/\1/" |
		sort -n |
		tail -1 ||
		true
)"

next=$((${highest:-0} + 1))
version="v${slot}.${next}"

if git rev-parse -q --verify "refs/tags/${version}" >/dev/null; then
	echo "next-version: ${version} is already tagged" >&2
	exit 1
fi

printf '%s\n' "${version}"
