#!/usr/bin/env node
const args = process.argv.slice(2)
if (args[0] === "route") {
  process.stdout.write(JSON.stringify({ target_id: "claude-haiku", effort: "low", reason: "mechanical task" }))
} else if (args[0] === "run") {
  process.stdout.write(JSON.stringify(args))
} else {
  process.exitCode = 1
}
