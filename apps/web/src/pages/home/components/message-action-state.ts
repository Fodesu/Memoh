import type { ChatAssistantTurn, ChatMessage, ContentBlock } from '@/store/chat-list'
import { hasPendingAssistantDecision } from '@/store/chat/pending-decisions'

export interface AssistantActionState {
  blocked: boolean
}

function hasVisibleAssistantBlock(block: ContentBlock): boolean {
  if (block.type === 'text' || block.type === 'error') return Boolean(block.content)
  if (block.type === 'attachments') return block.attachments.length > 0
  return true
}

// Actions belong to a logical user turn, not to each assistant projection in
// that turn. Continuations can temporarily produce more than one assistant
// row, but an unfinished turn must never expose or reserve an action row.
export function assistantActionOwner(
  messages: readonly ChatMessage[],
  state: AssistantActionState,
): ChatAssistantTurn | null {
  const assistants = messages.filter((message): message is ChatAssistantTurn => message.role === 'assistant')
  if (state.blocked || assistants.some(message =>
    message.streaming || message.__optimistic || hasPendingAssistantDecision(message),
  )) return null

  for (let index = assistants.length - 1; index >= 0; index -= 1) {
    const assistant = assistants[index]
    if (assistant?.messages.some(hasVisibleAssistantBlock)) return assistant
  }
  return null
}
