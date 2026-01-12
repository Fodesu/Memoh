import { z } from 'zod'

export const AgentStreamModel = {
  body: z.object({
    message: z.string().min(1, 'Message is required'),
    // Optional overrides - if not provided, will use settings
    maxContextLoadTime: z.number().int().min(1).max(1440).optional(),
    language: z.string().optional(),
    platform: z.string().optional(),
  }),
}

export type AgentStreamInput = z.infer<typeof AgentStreamModel['body']>

