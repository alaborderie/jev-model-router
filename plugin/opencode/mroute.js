import { tool } from "@opencode-ai/plugin"
import { createRouteTools } from "./lib/route-core.mjs"

export const MroutePlugin = async ({ client }) => ({
  tool: createRouteTools(tool, client),
})
