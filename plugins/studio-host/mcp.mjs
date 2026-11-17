import { join } from 'node:path'

export const name = 'horizon-studio-mcp'
export const inject = ['mcpManager', 'systemPrompt']

export function apply(ctx) {
  const root = process.env.HORIZONSTUDIO_ROOT
  if (!root) throw new Error('HorizonStudio root is required to start the Studio MCP adapter')

  ctx.mcpManager.register('studio', {
    transport: 'stdio',
    command: process.execPath,
    args: [join(root, 'scripts', 'studio-mcp.mjs')],
    cwd: root,
    env: {
      HORIZON_STUDIO_MCP_TOKEN: '${credential:HORIZON_STUDIO_MCP_TOKEN}',
      ...(process.env.HORIZONSTUDIO_API ? { HORIZONSTUDIO_API: process.env.HORIZONSTUDIO_API } : {}),
      ...(process.env.API_ADDR ? { API_ADDR: process.env.API_ADDR } : {}),
    },
  })

  ctx.systemPrompt.context({
    name: 'studio:session-id',
    order: ctx.systemPrompt.getContextOrder('WORKING_DIRECTORY') + 1,
    interpolate: false,
    text: ({ agent }) => agent ? `Current DSH session ID: ${JSON.stringify(agent.session.id)}.` : '',
  })
}
