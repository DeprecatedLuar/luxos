#!/usr/bin/env bash
# Run every suite; exit non-zero if any fails. Fixtures live in mktemp dirs
# and are removed on exit — nothing touches CONFIG_DIR, STAGING_DIR or /etc.
set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUITES=(parse.sh boundary.sh names.sh retarget.sh modules-function.sh)

status=0
for suite in "${SUITES[@]}"; do
    printf '\n\033[1;36m##### %s #####\033[0m\n' "$suite"
    bash "$TESTS_DIR/$suite" || status=1
done
exit $status
