import assert from "node:assert/strict"
import { fileURLToPath } from "node:url"
import { test } from "node:test"
import { createRouteTools } from "./lib/route-core.mjs"

const binary = [process.execPath, fileURLToPath(new URL("./testdata/mroute.mjs", import.meta.url))]
const tool = (definition) => definition
tool.schema = { string: () => ({ describe: () => "prompt" }) }

test("route passes a flag-prefixed prompt as one literal argv element", async () => {
  const tools = createRouteTools(tool, {}, binary)
  const output = await tools.route.execute({ prompt: "--json; $(touch /bad)" }, { directory: process.cwd() })
  assert.deepEqual(JSON.parse(output), ["run", "--json; $(touch /bad)"])
})

test("recommend displays the route without changing the active model", async () => {
  const messages = []
  const client = { tui: {
    appendPrompt: async ({ body }) => { messages.push(body.text) },
    showToast: async ({ body }) => { messages.push(body.message) },
  } }
  const tools = createRouteTools(tool, client, binary)
  const output = await tools.recommend.execute({ prompt: "Fix the typo" }, { directory: process.cwd() })
  assert.match(output, /claude-haiku @ low/)
  assert.equal(messages.length, 2)
  assert.ok(messages.every((message) => message.includes("claude-haiku @ low")))
  assert.ok(messages.every((message) => message.includes("one-shot")))
})

test("recommend remains usable without a TUI", async () => {
  const tools = createRouteTools(tool, { tui: { appendPrompt: async () => { throw new Error("unavailable") } } }, binary)
  assert.match(await tools.recommend.execute({ prompt: "Fix typo" }, { directory: process.cwd() }), /claude-haiku @ low/)
})

test("blank prompts are rejected before executing", async () => {
  const tools = createRouteTools(tool, {}, binary)
  await assert.rejects(() => tools.route.execute({ prompt: "   " }, { directory: process.cwd() }), /prompt is required/)
})

test("a delegated task can outlive the recommendation budget", async () => {
  const tools = createRouteTools(tool, {}, binary)
  await assert.rejects(() => tools.recommend.execute({ prompt: "slow" }, { directory: process.cwd() }), /timed out|timeout/i)
  assert.deepEqual(JSON.parse(await tools.route.execute({ prompt: "slow" }, { directory: process.cwd() })), ["run", "slow"])
})

test("large delegated output is truncated without killing the task", async () => {
  const tools = createRouteTools(tool, {}, binary)
  const output = await tools.route.execute({ prompt: "large" }, { directory: process.cwd() })
  assert.ok(output.startsWith("xxx"))
  assert.match(output, /output truncated/)
  assert.ok(output.length < 11 * 1024 * 1024)
})

test("cancelling the tool stops its delegated process", async () => {
  const tools = createRouteTools(tool, {}, binary)
  const controller = new AbortController()
  const started = Date.now()
  const pending = tools.route.execute({ prompt: "slow" }, { directory: process.cwd(), abort: controller.signal })
  setTimeout(() => controller.abort(), 100)
  await assert.rejects(pending, /abort|cancel/i)
  assert.ok(Date.now() - started < 1500, "cancellation should not wait for the slow task")
})
