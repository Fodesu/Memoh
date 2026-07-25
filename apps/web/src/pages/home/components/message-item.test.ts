// @vitest-environment jsdom
/* eslint-disable vue/one-component-per-file */

import { createApp, defineComponent, h, nextTick, ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ChatAssistantTurn, ContentBlock, ToolCallBlock } from '@/store/chat-list'
import { assistantActionOwner } from './message-action-state'

const SlotStub = defineComponent({
  name: 'SlotStub',
  setup(_, { slots }) {
    return () => h('div', slots.default?.())
  },
})

const EmptyStub = defineComponent({
  name: 'EmptyStub',
  setup() {
    return () => h('span')
  },
})

vi.mock('@felinic/ui', () => ({
  Avatar: SlotStub,
  AvatarImage: EmptyStub,
  AvatarFallback: SlotStub,
  Button: SlotStub,
  Textarea: EmptyStub,
}))

vi.mock('lucide-vue-next', () => ({
  CircleAlert: EmptyStub,
  Sparkles: EmptyStub,
}))

vi.mock('markstream-vue', () => ({
  default: EmptyStub,
  enableKatex: vi.fn(),
  enableMermaid: vi.fn(),
  setCustomComponents: vi.fn(),
}))

vi.mock('@/components/markdown', () => ({ registerSharedMarkdownComponents: vi.fn() }))
vi.mock('@/components/themed-mermaid-block/index.vue', () => ({ default: EmptyStub }))
vi.mock('./chat-code-block.vue', () => ({ default: EmptyStub }))
vi.mock('./tool-call-group.vue', () => ({ default: EmptyStub }))
vi.mock('./attachment-block.vue', () => ({ default: EmptyStub }))
vi.mock('./collapsible-user-text.vue', () => ({ default: EmptyStub }))
vi.mock('./background-task-block.vue', () => ({ default: EmptyStub }))
vi.mock('./heartbeat-trigger-block.vue', () => ({ default: EmptyStub }))
vi.mock('./schedule-trigger-block.vue', () => ({ default: EmptyStub }))
vi.mock('@/components/chat-list/channel-badge/index.vue', () => ({ default: EmptyStub }))
vi.mock('./message-actions.vue', () => ({
  default: defineComponent({
    name: 'MessageActionsStub',
    setup() {
      return () => h('div', { 'data-message-actions': '' })
    },
  }),
}))

vi.mock('./tool-call-registry', () => ({ toolSegmentCategoryForBlock: () => 'action' }))
vi.mock('./reasoning-timing', () => ({ finalizeReasoning: vi.fn(), markReasoningSeen: vi.fn() }))
vi.mock('../composables/useMediaGallery', () => ({ resolveUrl: (value: string) => value }))
vi.mock('@/utils/date-time', () => ({
  formatRelativeTime: () => '',
  formatDateTime: () => '',
  formatCalendarTime: () => '',
}))
vi.mock('@/store/settings', () => ({
  useSettingsStore: () => ({ isDark: false, shikiThemeLight: '', shikiThemeDark: '' }),
}))
vi.mock('@/store/user', () => ({
  useUserStore: () => ({ userInfo: { avatarUrl: '', displayName: '', username: '' } }),
}))
vi.mock('@vueuse/core', () => ({ useElementVisibility: () => ref(false) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({
    t: (key: string) => key,
    tm: () => ['Thinking'],
    rt: (value: unknown) => String(value),
    locale: ref('en'),
  }),
}))

function toolBlock(values: Partial<ToolCallBlock> = {}): ToolCallBlock {
  return {
    id: 1,
    type: 'tool',
    name: 'exec',
    input: { command: 'true' },
    tool_call_id: 'tool-call-1',
    running: false,
    toolCallId: 'tool-call-1',
    toolName: 'exec',
    result: null,
    done: false,
    ...values,
  }
}

function assistantTurn(messages: ContentBlock[], values: Partial<ChatAssistantTurn> = {}): ChatAssistantTurn {
  return {
    id: 'assistant-1',
    role: 'assistant',
    messages,
    timestamp: '',
    streaming: false,
    ...values,
  }
}

describe('message item assistant actions', () => {
  let app: ReturnType<typeof createApp> | undefined
  let root: HTMLDivElement | undefined

  afterEach(() => {
    app?.unmount()
    root?.remove()
    app = undefined
    root = undefined
  })

  async function mountTurn(
    turn: ChatAssistantTurn,
    state: { blocked?: boolean } = {},
  ) {
    const MessageItem = (await import('./message-item.vue')).default
    root = document.createElement('div')
    document.body.append(root)
    app = createApp(MessageItem, {
      message: turn,
      showAssistantActions: assistantActionOwner([turn], {
        blocked: state.blocked ?? false,
      }) === turn,
      isScrolling: false,
      isLastMessage: true,
    })
    app.mount(root)
    await nextTick()
    return root
  }

  it.each([
    {
      name: 'tool approval is pending',
      turn: assistantTurn([toolBlock({
        approval: { approval_id: 'approval-1', status: 'pending', can_approve: true },
      })]),
      blocked: false,
    },
    {
      name: 'ask_user is pending',
      turn: assistantTurn([toolBlock({
        toolName: 'ask_user',
        userInput: {
          user_input_id: 'input-1',
          status: 'pending',
          can_respond: true,
          questions: [],
        },
      })]),
      blocked: false,
    },
    {
      name: 'a resolved Decision is resuming the latest turn',
      turn: assistantTurn([
        toolBlock({ approval: { approval_id: 'approval-1', status: 'approved', can_approve: false } }),
        { id: 2, type: 'text', content: 'Partial answer' },
      ]),
      blocked: true,
    },
    {
      name: 'the latest turn is streaming',
      turn: assistantTurn([{ id: 1, type: 'text', content: 'Partial answer' }], { streaming: true }),
      blocked: true,
    },
  ])('does not reserve an action row while $name', async ({ turn, blocked }) => {
    const el = await mountTurn(turn, { blocked })
    expect(el.querySelector('[data-message-actions]')).toBeNull()
    expect(el.querySelector('.chat-message-meta')).toBeNull()
  })

  it('mounts actions after the visible assistant turn reaches a terminal state', async () => {
    const turn = assistantTurn([
      toolBlock({ approval: { approval_id: 'approval-1', status: 'approved', can_approve: false } }),
      { id: 2, type: 'text', content: 'Completed answer' },
    ])

    const el = await mountTurn(turn)

    expect(el.querySelector('[data-message-actions]')).not.toBeNull()
  })

  it('keeps actions on an older terminal turn while a newer turn is active', async () => {
    const turn = assistantTurn([{ id: 1, type: 'text', content: 'Earlier answer' }])

    const el = await mountTurn(turn)

    expect(el.querySelector('[data-message-actions]')).not.toBeNull()
  })

  it('keeps one logical turn actionless through pending, resolution, and active continuation', () => {
    const decision = toolBlock({
      approval: { approval_id: 'approval-1', status: 'pending', can_approve: true },
    })
    const decisionTurn = assistantTurn([decision], { id: 'assistant-decision' })
    const messages: ChatAssistantTurn[] = [decisionTurn]

    expect(assistantActionOwner(messages, { blocked: false })).toBeNull()

    decision.approval = { approval_id: 'approval-1', status: 'approved', can_approve: false }
    expect(assistantActionOwner(messages, { blocked: true })).toBeNull()

    const continuationTurn = assistantTurn(
      [{ id: 2, type: 'text', content: 'Continuation output' }],
      { id: 'assistant-continuation', streaming: true },
    )
    messages.push(continuationTurn)
    expect(assistantActionOwner(messages, { blocked: true })).toBeNull()

    continuationTurn.streaming = false
    expect(assistantActionOwner(messages, { blocked: false })).toBe(continuationTurn)
  })

  it('does not render an empty terminal assistant or an action slot', async () => {
    const el = await mountTurn(assistantTurn([]))

    expect(el.children).toHaveLength(0)
    expect(el.querySelector('[data-message-actions]')).toBeNull()
  })

  it('does not synthesize thinking while a Decision continuation awaits its first step', async () => {
    const el = await mountTurn(assistantTurn([], {
      streaming: true,
      __decisionContinuation: true,
    }))

    expect(el.children).toHaveLength(0)
    expect(el.textContent).not.toContain('Thinking')
  })

  it('keeps the initial thinking indicator for a new empty streaming turn', async () => {
    const el = await mountTurn(assistantTurn([], { streaming: true }))

    expect(el.textContent).toContain('Thinking')
  })
})
