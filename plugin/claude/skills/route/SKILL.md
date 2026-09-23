---
name: route
description: Run a standalone coding task through mroute's selected engine
disable-model-invocation: true
---

Pass the following task exactly as the `prompt` argument to the `mroute` MCP
server's `route` tool, then return its output to the user:

$ARGUMENTS

This starts a separate one-shot CLI run. It does not change this conversation's
model. Do not interpolate the task into a shell command. If the `mroute` tool is
unavailable, tell the user to install `mroute` on their PATH, run `mroute init`,
and restart Claude Code.
