#!/usr/bin/env bash
#
# Build the granular release artifacts locally with GoReleaser, in snapshot mode
# so nothing is published. This mirrors what the `release` job in
# .github/workflows/ci.yml runs on a tag, letting you validate the build before
# pushing one.
#
# By default it runs `goreleaser build` (compiles the granular-client,
# granular-auth-server, granular-github-resource-server and granular-subject binaries for
# every target into ./dist). Pass --release to run the full `goreleaser release`
# instead, which also produces the archives, checksums and Docker images
# (multi-arch images need a docker-container buildx builder — see the hint printed
# below).
#
# Usage:
#   scripts/goreleaser-build.sh                      # build all targets
#   scripts/goreleaser-build.sh --single-target      # only the host OS/arch (fast)
#   scripts/goreleaser-build.sh --release            # full snapshot release (+docker)
#   scripts/goreleaser-build.sh -- <extra args...>   # forward args to goreleaser
#
set -euo pipefail

# Run from the repository root regardless of where the script is invoked from.
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

command="build"
args=()
for arg in "$@"; do
	case "$arg" in
		--release)
			command="release"
			;;
		--)
			# Everything after `--` is forwarded verbatim to goreleaser.
			;;
		*)
			args+=("$arg")
			;;
	esac
done

# Prefer an installed goreleaser; otherwise fall back to `go run` so the script
# works with no extra setup (the first run compiles goreleaser, which is slower).
if command -v goreleaser >/dev/null 2>&1; then
	runner=(goreleaser)
else
	echo "goreleaser not found on PATH; falling back to 'go run' (slower first time)." >&2
	echo "Install it for faster runs: https://goreleaser.com/install/" >&2
	runner=(go run github.com/goreleaser/goreleaser/v2@latest)
fi

if [ "$command" = "release" ]; then
	echo "Building a full snapshot release (binaries + archives + docker images)." >&2
	echo "Multi-arch images require a docker-container builder:" >&2
	echo "    docker buildx create --use" >&2
	exec "${runner[@]}" release --snapshot --clean "${args[@]}"
else
	exec "${runner[@]}" build --snapshot --clean "${args[@]}"
fi
