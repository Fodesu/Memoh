import { createAgent as createAgentService } from '@memohome/agent'
import { createMemory, filterByTimestamp, MemoryUnit } from '@memohome/memory'
import { ChatModel, EmbeddingModel, Platform, Schedule } from '@memohome/shared'
import { createSchedule, deleteSchedule, getActiveSchedules } from '../schedule/service'
import { getActivePlatforms, sendMessageToPlatform } from '../platform/service'

// Type for messages passed to onFinish callback
type MessageType = Record<string, unknown>

export interface CreateAgentStreamParams {
  userId: string
  chatModel: ChatModel
  embeddingModel: EmbeddingModel
  summaryModel: ChatModel
  maxContextLoadTime?: number
  language?: string
  platform?: string
  onFinish?: (messages: MessageType[]) => Promise<void>
}

export async function createAgent(params: CreateAgentStreamParams) {
  const {
    userId,
    chatModel,
    embeddingModel,
    summaryModel,
    maxContextLoadTime,
    language,
    platform,
    onFinish,
  } = params

  // Create memory instance
  const memoryInstance = createMemory({
    summaryModel,
    embeddingModel,
  })

  const platforms = await getActivePlatforms()

  // Create agent
  const agent = createAgentService({
    model: chatModel,
    maxContextLoadTime,
    language: language || 'Same as user input',
    platforms: platforms as Platform[],
    currentPlatform: platform,
    onSendMessage: async (platform: string, options) => {
      await sendMessageToPlatform(platform, {
        message: options.message,
        userId,
      })
    },
    onReadMemory: async (from: Date, to: Date) => {
      return await filterByTimestamp(from, to, userId)
    },
    onSearchMemory: async (query: string) => {
      const results = await memoryInstance.searchMemory(query, userId)
      return results
    },
    onFinish: async (messages: MessageType[]) => {
      // Save conversation to memory
      const memoryUnit: MemoryUnit = {
        messages: messages as unknown as MemoryUnit['messages'],
        timestamp: new Date(),
        user: userId,
      }
      await memoryInstance.addMemory(memoryUnit)
      
      // Call custom onFinish handler if provided
      await onFinish?.(messages)
    },
    onGetSchedules: async () => {
      const schedules = await getActiveSchedules(userId)
      return schedules.map(schedule => ({
        id: schedule.id!,
        pattern: schedule.pattern,
        name: schedule.name,
        description: schedule.description,
        command: schedule.command,
        maxCalls: schedule.maxCalls || undefined,
      }))
    },
    onRemoveSchedule: async (id: string) => {
      await deleteSchedule(id, userId)
    },
    onSchedule: async (schedule: Schedule) => {
      await createSchedule(userId, {
        name: schedule.name,
        description: schedule.description,
        command: schedule.command,
        pattern: schedule.pattern,
        maxCalls: schedule.maxCalls || undefined,
      })
    },
  })

  return agent
}

