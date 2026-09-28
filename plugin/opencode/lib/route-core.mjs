import { execFile } from "node:child_process"
import { promisify } from "node:util"

const execute = promisify(execFile)

async function mroute(binary, args, directory) {
  const [file, ...prefix] = Array.isArray(binary) ? binary : [binary]
  const { stdout } = await execute(file, [...prefix, ...args], {
    cwd: directory,
    timeout: 120_000,
    maxBuffer: 10 * 1024 * 1024,
  })
  return stdout
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
        return mroute(binary, ["run", promptFrom(input)], context.directory)
      },
    }),
    recommend: tool({
      description: "Recommend a model for a separate one-shot task, without switching this conversation.",
      args,
      async execute(input, context) {
        const prompt = promptFrom(input)
        const decision = JSON.parse(await mroute(binary, ["route", "--json", "--", prompt], context.directory))
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
