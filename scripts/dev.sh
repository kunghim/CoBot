#!/usr/bin/env bash
# Node is a developer prerequisite; the installed product remains standalone.
set -euo pipefail
exec node "$(dirname "$0")/dev.mjs" "$@"
