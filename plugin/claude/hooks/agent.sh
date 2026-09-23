#!/bin/sh
command -v mroute >/dev/null 2>&1 || exit 0
mroute hook-agent 2>/dev/null || exit 0
