#!/usr/bin/env bash

set -euo pipefail

tag=${1:-}
if [[ ! $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?-[0-9]+$ ]]; then
  echo "invalid fork release tag: '$tag'" >&2
  echo "expected an upstream version plus numeric fork suffix, for example v1.0.0-rc.23-0 or v0.13.2-0" >&2
  exit 64
fi

case "$tag" in
  v1.*|v0.13.*)
    ;;
  *)
    echo "unsupported fork release track: '$tag'" >&2
    echo "supported tracks are v1.x and v0.13.x" >&2
    exit 64
    ;;
esac

printf '%s\n' "$tag"
