#!/bin/sh
# Missing binaries and all routing failures are silent, successful no-ops.
command -v mroute >/dev/null 2>&1 || exit 0
mroute hook-prompt 2>/dev/null || exit 0
