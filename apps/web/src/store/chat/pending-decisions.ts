import type { ChatAssistantTurn, ChatMessage } from './types'

export function hasPendingAssistantDecision(message: ChatAssistantTurn): boolean {
  return message.messages.some(block => block.type === 'tool' && (
    Boolean(block.approval?.approval_id && block.approval.status === 'pending')
    || Boolean(block.userInput?.user_input_id && block.userInput.status === 'pending')
  ))
}

export function hasPendingChatDecision(messages: readonly ChatMessage[]): boolean {
  for (const message of messages) {
    if (message.role !== 'assistant') continue
    if (hasPendingAssistantDecision(message)) return true
  }
  return false
}
