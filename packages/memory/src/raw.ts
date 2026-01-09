import { ModelMessage } from 'ai'
import { MemoryUnit } from './memory-unit'

export const rawMessages = (messages: ModelMessage[]) => {
  return messages.map((message) => {
    if (message.role === 'user') {
      if (Array.isArray(message.content)) {
        return `User: ${message.content.filter(c => c.type === 'text').map(c => c.text).join('\n')}`
      }
      return `User: ${message.content}`
    } else if (message.role === 'assistant') {
      if (Array.isArray(message.content)) {
        return `You: ${message.content.map(m => {
          if (m.type === 'text') {
            return m.text
          } else if (m.type === 'tool-call') {
            return `[Tool Call: ${m.toolName}]`
          } else {
            return ''
          }
        }).join('\n')}`
      }
      return `You: ${message.content}`
    } else if (message.role === 'tool') {
      return `Tool Result: ${message.content}`
    } else {
      return null
    }
  })
  .filter((message) => message !== null)
  .join('\n\n')
}

export const rawMemory = (memory: MemoryUnit, locale?: Intl.LocalesArgument) => {
  return `
  ---
  date: ${memory.timestamp.toLocaleDateString(locale)}
  time: ${memory.timestamp.toLocaleTimeString(locale)}
  timezone: ${memory.timestamp.getTimezoneOffset()}
  ---
  ${rawMessages(memory.messages)}
  `.trim()
}
