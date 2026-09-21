import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import type { UIMessage, UITurn } from '@/composables/api/useChat.types'
import { createBackgroundTaskTracker } from './background-tasks'
import { createTranscriptController } from './transcript'
import type { RuntimeTranscriptSlice } from './runtime-projection'

vi.mock('@/store/user', () => ({
  useUserStore: () => ({ userInfo: { id: 'user-1' } }),
}))

function rawUser(id: string, text = 'hello'): UITurn {
  return { id, turn_id: 'turn-1', role: 'user', text, timestamp: '2026-01-01T00:00:00.000Z', platform: 'local' }
}

function rawAssistant(id: string, messages: UIMessage[] = []): UITurn {
  return { id, turn_id: 'turn-1', role: 'assistant', messages, timestamp: '2026-01-01T00:00:01.000Z' }
}

function makeTranscript() {
  const backgroundTasks = createBackgroundTaskTracker()
  return createTranscriptController({
    currentBotId: ref<string | null>('bot-1'),
    sessionId: ref<string | null>('session-1'),
    rememberBackgroundTask: backgroundTasks.rememberBackgroundTask,
    applyPendingBackgroundEventsToTool: backgroundTasks.applyPendingBackgroundEventsToTool,
    bumpFsChangedAtIfFsMutation: vi.fn(),
    fetchMessages: vi.fn().mockResolvedValue([]),
    locateMessage: vi.fn().mockResolvedValue({ items: [], target_id: '', target_external_message_id: '' }),
  })
}

const errorBlock: UIMessage = { id: 0, type: 'error', code: 'agent.provider_auth_failed', content: 'rejected' }

function settledUnpersisted(overrides: Partial<RuntimeTranscriptSlice> = {}): RuntimeTranscriptSlice {
  return {
    runId: 'run-1',
    turnId: 'turn-1',
    invocationId: 'invocation-1',
    status: 'errored',
    operation: null,
    streaming: false,
    unpersisted: true,
    turns: [rawUser('runtime-user'), rawAssistant('runtime-assistant', [errorBlock])],
    ...overrides,
  }
}

// A run that settled without writing a history turn is an unsent send. The
// frame still settles what the sender is looking at, but a transcript that
// never saw the run live must not gain a round history does not have.
describe('unpersisted settled run frames', () => {
  it('settles the turns already on screen', () => {
    const transcript = makeTranscript()
    const assistantTurn = transcript.createOptimisticAssistantTurn('invocation-1')
    const userTurn = transcript.createOptimisticUserTurn('hello', undefined, 'invocation-1')
    transcript.appendToView(userTurn, assistantTurn)

    expect(transcript.applyRuntimeTranscript(settledUnpersisted())).toBe(true)

    expect(transcript.messages.map(turn => turn.role)).toEqual(['user', 'assistant'])
    expect(assistantTurn.streaming).toBe(false)
    expect(assistantTurn.messages.map(block => block.type)).toEqual(['error'])
  })

  it('introduces no turns into a transcript that never saw the run live', () => {
    const transcript = makeTranscript()
    transcript.replaceMessages([
      { id: 'user-0', turn_id: 'turn-0', role: 'user', text: 'earlier', timestamp: '2025-12-31T00:00:00.000Z' },
      { id: 'assistant-0', turn_id: 'turn-0', role: 'assistant', messages: [], timestamp: '2025-12-31T00:00:01.000Z' },
    ], 'session-1')

    expect(transcript.applyRuntimeTranscript(settledUnpersisted({ invocationId: '' }))).toBe(true)

    expect(transcript.messages.map(turn => turn.id)).toEqual(['user-0', 'assistant-0'])
  })

  it('does not cut the tail a failed replacement never replaced', () => {
    const transcript = makeTranscript()
    transcript.replaceMessages([
      { id: 'user-0', turn_id: 'turn-0', role: 'user', text: 'earlier', timestamp: '2025-12-31T00:00:00.000Z' },
      { id: 'assistant-0', turn_id: 'turn-0', role: 'assistant', messages: [], timestamp: '2025-12-31T00:00:01.000Z' },
    ], 'session-1')

    expect(transcript.applyRuntimeTranscript(settledUnpersisted({
      invocationId: '',
      operation: { kind: 'retry', replace_from_message_id: 'assistant-0' },
    }))).toBe(true)

    expect(transcript.messages.map(turn => turn.id)).toEqual(['user-0', 'assistant-0'])
  })

  it('still appends a settled run whose turn reached history', () => {
    const transcript = makeTranscript()
    transcript.replaceMessages([
      { id: 'user-0', turn_id: 'turn-0', role: 'user', text: 'earlier', timestamp: '2025-12-31T00:00:00.000Z' },
    ], 'session-1')

    expect(transcript.applyRuntimeTranscript(settledUnpersisted({ invocationId: '', unpersisted: false }))).toBe(true)

    expect(transcript.messages.map(turn => turn.id)).toEqual(['user-0', 'runtime-user', 'runtime-assistant'])
  })
})
