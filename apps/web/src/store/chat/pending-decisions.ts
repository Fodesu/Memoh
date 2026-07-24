import type { ChatMessage } from './types'

export function hasPendingChatDecision(messages: readonly ChatMessage[]): boolean {
  for (const message of messages) {
    if (message.role !== 'assistant') continue
    for (const block of message.messages) {
      if (block.type !== 'tool') continue
      if (block.approval?.approval_id && block.approval.status === 'pending') return true
      if (block.userInput?.user_input_id && block.userInput.status === 'pending') return true
    }
  }
  return false
}
