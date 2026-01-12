import type { MemoryUnit } from '@memohome/memory'
import { ChatModel, Platform, Schedule } from '@memohome/shared'
import { ModelMessage } from 'ai'

export interface SendMessageOptions {
  message: string
}

export interface AgentParams {
  model: ChatModel

  /**
   * Unit: minutes
   */
  maxContextLoadTime?: number

  locale?: Intl.LocalesArgument

  /**
   * Preferred language of the assistant.
   * @default 'Same as user input'
   */
  language?: string

  platforms?: Platform[]

  currentPlatform?: string

  onSendMessage?: (platform: string, options: SendMessageOptions) => Promise<void>

  onReadMemory?: (from: Date, to: Date) => Promise<MemoryUnit[]>

  onSearchMemory?: (query: string) => Promise<object[]>

  onSchedule?: (schedule: Schedule) => Promise<void>

  onGetSchedules?: () => Promise<Schedule[]>

  onRemoveSchedule?: (id: string) => Promise<void>

  onFinish?: (messages: ModelMessage[]) => Promise<void>

  onError?: (error: Error) => Promise<void>
}