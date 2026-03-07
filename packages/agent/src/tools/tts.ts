import { tool } from 'ai'
import { z } from 'zod'
import type { AgentAuthContext, AuthFetcher, IdentityContext } from '../types'

export interface TtsToolParams {
  fetch: AuthFetcher
  auth: AgentAuthContext
  identity: IdentityContext
}

export const getTtsTools = ({ fetch, auth, identity }: TtsToolParams) => {
  const botId = identity.botId.trim()
  const baseUrl = auth.baseUrl.replace(/\/$/, '')

  const textToSpeech = tool({
    description:
      'Convert text to speech audio and send as a voice message. Use when the user asks you to speak, read aloud, or send a voice message.',
    inputSchema: z.object({
      text: z
        .string()
        .max(500)
        .describe('The text to convert to speech (max 500 characters)'),
    }),
    execute: async ({ text }) => {
      if (!botId || !baseUrl) {
        throw new Error('TTS requires bot identity and base URL')
      }
      const res = await fetch(`${baseUrl}/bots/${botId}/tts/synthesize`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text }),
      })
      if (!res.ok) {
        const body = await res.text()
        throw new Error(`TTS synthesis failed: ${res.status} ${body}`)
      }
      return await res.json()
    },
  })

  return {
    text_to_speech: textToSpeech,
  }
}
