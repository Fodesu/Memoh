import { Platform } from '@memohome/shared'
import { SendMessageOptions } from '../types'
import { tool } from 'ai'
import z from 'zod'

export const getMessageTools = (
  platforms: Platform[],
  onSendMessage: (platform: string, options: SendMessageOptions) => Promise<void>,
) => {
  const sendMessageTool = tool({
    description: 'Send a message to a platform',
    inputSchema: z.object({
      platform: z.enum(platforms.map(platform => platform.name)),
      message: z.string(),
    }),
    execute: async ({ platform, message }) => {
      await onSendMessage(platform, { message })
    },
  })

  return {
    'send-message': sendMessageTool,
  }
}