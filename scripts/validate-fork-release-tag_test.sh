#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
validator="$script_dir/validate-fork-release-tag.sh"

for tag in v1.0.0-rc.23-0 v1.0.0-rc.23-12 v0.13.2-0 v0.13.2-9; do
  "$validator" "$tag" >/dev/null
done

for tag in v1.0.0-rc.23 v0.13.2 latest nightly v2.0.0-0; do
  if "$validator" "$tag" >/dev/null 2>&1; then
    echo "validator unexpectedly accepted '$tag'" >&2
    exit 1
  fi
done

echo "fork release tag validation passed"
