import { streamText, ModelMessage, stepCountIs } from 'ai'
import { AgentParams } from './types'
import { system } from './prompts'
import { getMemoryTools } from './tools'
import { MemoryUnit } from '@memohome/memory'
import { createGateway } from './gateway'

export const createAgent = (params: AgentParams) => {
  const messages: ModelMessage[] = []
  const memory: MemoryUnit[] = []

  const gateway = createGateway(params.model)

  const getTools = async () => {
    return {
      ...getMemoryTools({
        searchMemory: params.onSearchMemory ?? (() => Promise.resolve([])),
        onLoadMemory: async (memory) => {
          memory.push(...memory)
        },
      }),
    }
  }

  const loadContext = async () => {
    const from = new Date(Date.now() - params.maxContextLoadTime * 60 * 1000)
    const to = new Date()
    const memory = await params.onReadMemory?.(from, to) ?? []
    const context = memory.flatMap(m => m.messages)
    messages.unshift(...context)
  }

  const getSystemPrompt = () => {
    return system({
      date: new Date(),
      language: params.language ?? 'Same as user input',
      locale: params.locale,
      maxContextLoadTime: params.maxContextLoadTime,
      memory,
    })
  }

  async function* ask(input: string) {
    await loadContext()
    const user = {
      role: 'user',
      content: input,
    }
    messages.push(user)
    const { fullStream, response } = streamText({
      model: gateway,
      system: getSystemPrompt(),
      prepareStep: async () => {
        return {
          system: getSystemPrompt(),
        }
      },
      stopWhen: stepCountIs(10),
      messages,
      tools: await getTools(),
    })
    for await (const event of fullStream) {
      yield event
    }
    const newMessages = (await response).messages
    await params.onFinish?.([
      user as ModelMessage,
      ...newMessages,
    ])
  }

  return {
    ask,
    loadContext,
    getSystemPrompt,
  }
}