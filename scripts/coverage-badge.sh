#!/usr/bin/env bash
#
# Render a self-contained coverage badge (a flat-style SVG) from a coverage
# percentage, with no external service. The CI `test` job calls this on a push to
# main to refresh .github/badges/coverage.svg, which the README embeds; you can
# also run it locally after `make cover`.
#
# The percentage may be given as an argument, or read from a Go coverage profile
# with --from-profile. The colour follows the usual coverage thresholds.
#
# Usage:
#   scripts/coverage-badge.sh 81.0 [out.svg]            # from a literal percent
#   scripts/coverage-badge.sh --from-profile coverage.out [out.svg]
#
set -euo pipefail

out="${OUT:-.github/badges/coverage.svg}"

if [ "${1:-}" = "--from-profile" ]; then
	profile="${2:?--from-profile needs a coverage profile path}"
	# The `total:` line looks like: "total:	(statements)	81.0%".
	pct="$(go tool cover -func="$profile" | awk '/^total:/ {print $NF}')"
	[ "${3:-}" != "" ] && out="$3"
else
	pct="${1:?usage: coverage-badge.sh <percent> [out.svg]  (or --from-profile <profile>)}"
	[ "${2:-}" != "" ] && out="$2"
fi

# Normalise to a bare number with one decimal, e.g. "81.0%" or "81" -> "81.0".
pct="${pct%\%}"
num="$(awk -v p="$pct" 'BEGIN { printf "%.1f", p }')"
label_value="${num}%"

# Colour by threshold, mirroring shields.io's coverage palette.
color="$(awk -v n="$num" 'BEGIN {
	if (n >= 90)      print "#4c1";   # brightgreen
	else if (n >= 80) print "#97ca00"; # green
	else if (n >= 70) print "#a4a61d"; # yellowgreen
	else if (n >= 60) print "#dfb317"; # yellow
	else if (n >= 50) print "#fe7d37"; # orange
	else              print "#e05d44"; # red
}')"

# Geometry: a fixed label box ("coverage") and a value box sized to its text.
label_w=61
value_w="$(awk -v s="$label_value" 'BEGIN { print int(length(s) * 7 + 10) }')"
total_w="$((label_w + value_w))"
label_x="$((label_w * 10 / 2))"
value_x="$(( (label_w * 10) + (value_w * 10 / 2) ))"
label_tl="$(( (label_w - 10) * 10 ))"
value_tl="$(( (value_w - 10) * 10 ))"

mkdir -p "$(dirname "$out")"
cat > "$out" <<EOF
<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="${total_w}" height="20" role="img" aria-label="coverage: ${label_value}">
  <title>coverage: ${label_value}</title>
  <linearGradient id="s" x2="0" y2="100%">
    <stop offset="0" stop-color="#bbb" stop-opacity=".1"/>
    <stop offset="1" stop-opacity=".1"/>
  </linearGradient>
  <clipPath id="r"><rect width="${total_w}" height="20" rx="3" fill="#fff"/></clipPath>
  <g clip-path="url(#r)">
    <rect width="${label_w}" height="20" fill="#555"/>
    <rect x="${label_w}" width="${value_w}" height="20" fill="${color}"/>
    <rect width="${total_w}" height="20" fill="url(#s)"/>
  </g>
  <g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" text-rendering="geometricPrecision" font-size="110">
    <text aria-hidden="true" x="${label_x}" y="150" fill="#010101" fill-opacity=".3" transform="scale(.1)" textLength="${label_tl}">coverage</text>
    <text x="${label_x}" y="140" transform="scale(.1)" textLength="${label_tl}">coverage</text>
    <text aria-hidden="true" x="${value_x}" y="150" fill="#010101" fill-opacity=".3" transform="scale(.1)" textLength="${value_tl}">${label_value}</text>
    <text x="${value_x}" y="140" transform="scale(.1)" textLength="${value_tl}">${label_value}</text>
  </g>
</svg>
EOF

echo "wrote ${out} (coverage ${label_value})"
