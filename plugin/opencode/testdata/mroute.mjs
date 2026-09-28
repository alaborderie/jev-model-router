#!/usr/bin/env node
const args = process.argv.slice(2)
if (args.at(-1) === "slow") {
  await new Promise((resolve) => setTimeout(resolve, 2200))
}
if (args[0] === "route") {
  process.stdout.write(JSON.stringify({ target_id: "claude-haiku", effort: "low", reason: "mechanical task" }))
} else if (args[0] === "run") {
  process.stdout.write(args[1] === "large" ? "x".repeat(11 * 1024 * 1024) : JSON.stringify(args))
} else {
  process.exitCode = 1
}
