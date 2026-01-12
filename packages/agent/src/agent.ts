import { streamText, generateText, ModelMessage, stepCountIs, UserModelMessage } from 'ai'
import { AgentParams } from './types'
import { system, schedule as schedulePrompt } from './prompts'
import { getMemoryTools, getScheduleTools, getMessageTools } from './tools'
import { createChatGateway } from '@memoh/ai-gateway'
import { Schedule } from '@memoh/shared'

export const createAgent = (params: AgentParams) => {
  const messages: ModelMessage[] = []

  const gateway = createChatGateway(params.model)

  const maxContextLoadTime = params.maxContextLoadTime ?? 24 * 60 // 24 hours
  const language = params.language ?? 'Same as user input'
  const platforms = params.platforms ?? []
  const currentPlatform = params.platforms
    ? platforms.find(p => p.name === params.currentPlatform)?.name ?? 'Unknown Platform'
    : 'client'

  const getTools = async () => {
    return {
      ...getMemoryTools({
        searchMemory: params.onSearchMemory ?? (() => Promise.resolve([]))
      }),
      ...getScheduleTools({
        onGetSchedules: params.onGetSchedules ?? (() => Promise.resolve([])),
        onRemoveSchedule: params.onRemoveSchedule ?? (() => Promise.resolve()),
        onSchedule: params.onSchedule ?? (() => Promise.resolve()),
      }),
      ...getMessageTools(
        platforms,
        params.onSendMessage ?? (() => Promise.resolve())
      ),
    }
  }

  const loadContext = async () => {
    const from = new Date(Date.now() - maxContextLoadTime * 60 * 1000)
    const to = new Date()
    const memory = await params.onReadMemory?.(from, to) ?? []
    const context = memory.flatMap(m => m.messages)
    messages.unshift(...context)
  }

  const getSystemPrompt = () => {
    return system({
      date: new Date(),
      language,
      locale: params.locale,
      maxContextLoadTime,
      platforms,
      currentPlatform,
    })
  }

  const getSchedulePrompt = (schedule: Schedule) => {
    return schedulePrompt({
      schedule,
      locale: params.locale,
      date: new Date(),
    })
  }

  async function askDirectly(input: string) {
    await loadContext()
    const user = {
      role: 'user',
      content: input,
    } as UserModelMessage
    messages.push(user)
    const { response } = await generateText({
      model: gateway,
      system: getSystemPrompt(),
      messages,
      tools: await getTools(),
    })
    await params.onFinish?.([
      user as ModelMessage,
      ...response.messages,
    ])
  }

  async function* ask(input: string) {
    await loadContext()
    const user = {
      role: 'user',
      content: input,
    } as UserModelMessage
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

  const triggerSchedule = async (schedule: Schedule) => {
    const prompt = getSchedulePrompt(schedule)
    await askDirectly(prompt)
  }

  return {
    ask,
    askDirectly,
    loadContext,
    getSystemPrompt,
    getSchedulePrompt,
    triggerSchedule,
  }
}