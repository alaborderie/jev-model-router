import { execFile, spawn } from "node:child_process"
import { promisify } from "node:util"

const execute = promisify(execFile)
const outputLimit = 10 * 1024 * 1024

async function recommend(binary, args, context) {
  const [file, ...prefix] = Array.isArray(binary) ? binary : [binary]
  try {
    const { stdout } = await execute(file, [...prefix, ...args], {
      cwd: context.directory,
      signal: context.abort,
      timeout: 2000,
      maxBuffer: 64 * 1024,
    })
    return stdout
  } catch (error) {
    if (error.killed && error.signal === "SIGTERM") throw new Error("recommendation timed out")
    if (context.abort?.aborted) throw new Error("recommendation cancelled")
    throw new Error("mroute route failed")
  }
}

function runOneShot(binary, prompt, context) {
  const [file, ...prefix] = Array.isArray(binary) ? binary : [binary]
  return new Promise((resolve, reject) => {
    const child = spawn(file, [...prefix, "run", prompt], { cwd: context.directory, signal: context.abort, stdio: ["ignore", "pipe", "pipe"] })
    const chunks = []
    let size = 0
    let truncated = false
    child.stdout.on("data", (chunk) => {
      const remaining = outputLimit - size
      if (chunk.length > remaining) truncated = true
      if (remaining > 0) {
        chunks.push(chunk.subarray(0, remaining))
        size += Math.min(chunk.length, remaining)
      }
    })
    // Drain stderr without retaining engine output that could include private task data.
    child.stderr.resume()
    child.on("error", (error) => reject(context.abort?.aborted ? new Error("route cancelled") : new Error(`mroute run failed: ${error.code || "spawn error"}`)))
    child.on("close", (code) => {
      if (context.abort?.aborted) return reject(new Error("route cancelled"))
      if (code !== 0) return reject(new Error(`mroute run exited with status ${code}`))
      resolve(Buffer.concat(chunks).toString("utf8") + (truncated ? "\n[output truncated at 10 MiB]\n" : ""))
    })
  })
}

function promptFrom(args) {
  if (typeof args.prompt !== "string" || !args.prompt.trim()) {
    throw new Error("prompt is required")
  }
  return args.prompt
}

export function createRouteTools(tool, client, binary = "mroute") {
  const args = { prompt: tool.schema.string().describe("Prompt to route verbatim") }
  return {
    route: tool({
      description: "Delegate a one-shot coding task through mroute run. Does not switch this conversation's model.",
      args,
      async execute(input, context) {
        return runOneShot(binary, promptFrom(input), context)
      },
    }),
    recommend: tool({
      description: "Recommend a model for a separate one-shot task, without switching this conversation.",
      args,
      async execute(input, context) {
        const prompt = promptFrom(input)
        const decision = JSON.parse(await recommend(binary, ["route", "--json", "--", prompt], context))
        const message = `mroute one-shot recommendation: ${decision.target_id} @ ${decision.effort} (${decision.reason}). This does not switch the current conversation's model.`
        await Promise.allSettled([
          client?.tui?.appendPrompt?.({ body: { text: message } }),
          client?.tui?.showToast?.({ body: { message, variant: "info" } }),
        ])
        return message
      },
    }),
  }
}
