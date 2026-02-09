import chalk from 'chalk'
import { readConfig, getBaseURL, TokenInfo } from '../utils/store'

// ---------------------------------------------------------------------------
// Tool display configuration
// ---------------------------------------------------------------------------

type ToolDisplayMode = 'inline' | 'expanded'

interface ToolDisplayConfig {
  mode: ToolDisplayMode
  /** For expanded mode: which parameter to show as detail content */
  expandParam?: string
  /** Label shown in the display header */
  label?: string
}

/**
 * Tools listed here will be displayed in "expanded" mode with a box showing
 * the specified parameter content. Everything else defaults to single-line.
 * exec uses a custom single-line format instead of the box.
 */
const TOOL_DISPLAY: Record<string, ToolDisplayConfig> = {
  exec:  { mode: 'expanded', label: 'exec' },
  write: { mode: 'expanded', expandParam: 'content', label: 'write' },
}

const getToolDisplay = (toolName: string): ToolDisplayConfig => {
  return TOOL_DISPLAY[toolName] ?? { mode: 'inline' }
}

// ---------------------------------------------------------------------------
// Tool call formatting helpers
// ---------------------------------------------------------------------------

const BOX_WIDTH = 60

// ---------------------------------------------------------------------------
// exec-specific helpers
// ---------------------------------------------------------------------------

/** Extract the actual shell command from exec input like { command: "bash", args: ["-lc", "echo hi"] } */
const extractExecCommand = (toolInput: unknown): string => {
  if (!toolInput || typeof toolInput !== 'object') return ''
  const input = toolInput as Record<string, unknown>
  const command = typeof input.command === 'string' ? input.command : ''
  const args = Array.isArray(input.args) ? input.args.map(String) : []
  // If shell + -c/-lc flag, extract the actual script
  if (/^(bash|sh|zsh)$/.test(command) && args.length >= 2) {
    const flag = args[0]
    if (flag === '-c' || flag === '-lc') {
      return args.slice(1).join(' ')
    }
  }
  return [command, ...args].filter(Boolean).join(' ')
}

const formatExecCall = (toolInput: unknown) => {
  const cmd = extractExecCommand(toolInput)
  return chalk.dim('  ▶ ') + chalk.white('$ ') + chalk.bold.white(cmd)
}

// ---------------------------------------------------------------------------
// edit-specific helpers
// ---------------------------------------------------------------------------

const extractEditInput = (toolInput: unknown) => {
  if (!toolInput || typeof toolInput !== 'object') {
    return { path: '', oldText: '', newText: '' }
  }
  const input = toolInput as Record<string, unknown>
  const path = typeof input.path === 'string' ? input.path : ''
  const oldText =
    typeof input.old_text === 'string'
      ? input.old_text
      : typeof input.oldText === 'string'
        ? input.oldText
        : ''
  const newText =
    typeof input.new_text === 'string'
      ? input.new_text
      : typeof input.newText === 'string'
        ? input.newText
        : ''
  return { path, oldText, newText }
}

const countLines = (text: string) => (text ? text.split('\n').length : 0)

const pushDetailBlock = (lines: string[], title: string, content: string) => {
  lines.push(chalk.cyan('│ ') + chalk.dim(title))
  const detailLines = content ? content.split('\n') : []
  if (!detailLines.length) {
    lines.push(chalk.cyan('│ ') + chalk.dim('∅'))
    return
  }
  const maxLines = 12
  const shown = detailLines.slice(0, maxLines)
  for (const dl of shown) {
    const truncated = dl.length > BOX_WIDTH - 4 ? dl.slice(0, BOX_WIDTH - 7) + '...' : dl
    lines.push(chalk.cyan('│ ') + chalk.white(truncated))
  }
  if (detailLines.length > maxLines) {
    lines.push(chalk.cyan('│ ') + chalk.dim(`... (${detailLines.length - maxLines} more lines)`))
  }
}

const formatEditCall = (toolInput: unknown) => {
  const { path, oldText, newText } = extractEditInput(toolInput)
  const oldLines = countLines(oldText)
  const newLines = countLines(newText)
  const summary = ` path: ${path || '(unknown)'} · old: ${oldLines} lines · new: ${newLines} lines`

  const topBorder = '┌' + '─'.repeat(BOX_WIDTH - 2) + '┐'
  const botBorder = '└' + '─'.repeat(BOX_WIDTH - 2) + '┘'

  const lines: string[] = []
  lines.push(chalk.cyan(topBorder))
  lines.push(chalk.cyan('│ ') + chalk.bold.white('edit') + chalk.gray(summary))
  lines.push(chalk.cyan('│ ') + chalk.dim('─'.repeat(BOX_WIDTH - 4)))
  pushDetailBlock(lines, 'old_text', oldText)
  lines.push(chalk.cyan('│ ') + chalk.dim('─'.repeat(BOX_WIDTH - 4)))
  pushDetailBlock(lines, 'new_text', newText)
  lines.push(chalk.cyan(botBorder))
  return lines.join('\n')
}

/** Try to unwrap MCP content-block results into a plain object */
const unwrapToolResult = (result: unknown): Record<string, unknown> | null => {
  if (!result) return null

  // Helper to extract from MCP content blocks array
  const extractFromContentBlocks = (arr: unknown[]): Record<string, unknown> | null => {
    for (const block of arr) {
      if (block && typeof block === 'object') {
        const b = block as Record<string, unknown>
        if (b.type === 'text' && typeof b.text === 'string') {
          try { return JSON.parse(b.text) } catch { /* ignore */ }
        }
      }
    }
    return null
  }

  // MCP content array: [{ type: "text", text: "{...}" }]
  if (Array.isArray(result)) {
    return extractFromContentBlocks(result)
  }

  // Object - might be MCP wrapper { content: [...] } or direct result
  if (typeof result === 'object') {
    const obj = result as Record<string, unknown>
    // MCP wrapper: { content: [{ type: "text", text: "{...}" }], isError: ... }
    if (Array.isArray(obj.content)) {
      const extracted = extractFromContentBlocks(obj.content)
      if (extracted) return extracted
    }
    // Direct object with known result fields
    return obj
  }

  // JSON string
  if (typeof result === 'string') {
    try { return JSON.parse(result) } catch { /* ignore */ }
  }
  return null
}

const formatExecResult = (result: unknown) => {
  const r = unwrapToolResult(result)
  if (!r) return chalk.dim('  ╰─ done')

  const exitCode = typeof r.exit_code === 'number' ? r.exit_code : (r.ok ? 0 : 1)
  const ok = exitCode === 0
  const stdout = typeof r.stdout === 'string' ? r.stdout.trim() : ''
  const stderr = typeof r.stderr === 'string' ? r.stderr.trim() : ''

  const lines: string[] = []
  lines.push(chalk.dim('  ╰─ ') + (ok ? chalk.green(`✓ exit ${exitCode}`) : chalk.red(`✗ exit ${exitCode}`)))

  const output = ok ? stdout : (stderr || stdout)
  if (output) {
    const outputLines = output.split('\n')
    const maxLines = 8
    const shown = outputLines.slice(0, maxLines)
    for (const ol of shown) {
      const truncated = ol.length > 72 ? ol.slice(0, 69) + '...' : ol
      lines.push(chalk.dim('    ') + (ok ? chalk.white(truncated) : chalk.yellow(truncated)))
    }
    if (outputLines.length > maxLines) {
      lines.push(chalk.dim(`    ... (${outputLines.length - maxLines} more lines)`))
    }
  }
  return lines.join('\n')
}

// ---------------------------------------------------------------------------
// Generic tool formatting
// ---------------------------------------------------------------------------

const formatToolCallInline = (toolName: string, toolInput: unknown) => {
  let params = ''
  if (toolInput && typeof toolInput === 'object') {
    const entries = Object.entries(toolInput as Record<string, unknown>)
    params = entries
      .map(([k, v]) => {
        const s = typeof v === 'string' ? v : JSON.stringify(v)
        const short = s.length > 40 ? s.slice(0, 37) + '...' : s
        return `${k}=${short}`
      })
      .join(', ')
  }
  return chalk.dim(`  ◆ ${toolName}`) + (params ? chalk.dim(`(${params})`) : '')
}

const formatToolCallExpanded = (config: ToolDisplayConfig, toolName: string, toolInput: unknown) => {
  const label = config.label ?? toolName
  const inputObj = (toolInput && typeof toolInput === 'object' ? toolInput : {}) as Record<string, unknown>

  const summaryParts: string[] = []
  for (const [k, v] of Object.entries(inputObj)) {
    if (k === config.expandParam) continue
    const s = typeof v === 'string' ? v : JSON.stringify(v)
    summaryParts.push(`${k}: ${s.length > 50 ? s.slice(0, 47) + '...' : s}`)
  }
  const summary = summaryParts.length ? ' ' + summaryParts.join(', ') : ''

  let detail = ''
  if (config.expandParam && config.expandParam in inputObj) {
    const raw = inputObj[config.expandParam]
    if (typeof raw === 'string') {
      detail = raw
    } else if (Array.isArray(raw)) {
      detail = raw.join(' ')
    } else {
      detail = JSON.stringify(raw, null, 2)
    }
  }

  const topBorder = '┌' + '─'.repeat(BOX_WIDTH - 2) + '┐'
  const botBorder = '└' + '─'.repeat(BOX_WIDTH - 2) + '┘'

  const lines: string[] = []
  lines.push(chalk.cyan(topBorder))
  lines.push(chalk.cyan('│ ') + chalk.bold.white(label) + chalk.gray(summary))

  if (detail) {
    lines.push(chalk.cyan('│ ') + chalk.dim('─'.repeat(BOX_WIDTH - 4)))
    const detailLines = detail.split('\n')
    const maxLines = 20
    const shown = detailLines.slice(0, maxLines)
    for (const dl of shown) {
      const truncated = dl.length > BOX_WIDTH - 4 ? dl.slice(0, BOX_WIDTH - 7) + '...' : dl
      lines.push(chalk.cyan('│ ') + chalk.white(truncated))
    }
    if (detailLines.length > maxLines) {
      lines.push(chalk.cyan('│ ') + chalk.dim(`... (${detailLines.length - maxLines} more lines)`))
    }
  }

  lines.push(chalk.cyan(botBorder))
  return lines.join('\n')
}

const formatToolResult = (toolName: string, result: unknown) => {
  // exec has its own result formatter
  if (toolName === 'exec') {
    return formatExecResult(result)
  }
  const config = getToolDisplay(toolName)
  if (config.mode === 'expanded' || toolName === 'edit') {
    const r = unwrapToolResult(result)
    if (r) {
      if ('ok' in r) {
        return chalk.dim(`  ╰─ `) + (r.ok ? chalk.green('✓ ok') : chalk.red('✗ failed'))
      }
    }
    return chalk.dim(`  ╰─ done`)
  }
  return null
}

// ---------------------------------------------------------------------------
// Text extraction helpers (fallback for unknown event formats)
// ---------------------------------------------------------------------------

const extractTextFromMessage = (message: unknown) => {
  if (typeof message === 'string') return message
  if (message && typeof message === 'object') {
    const value = message as { text?: unknown; parts?: unknown[] }
    if (typeof value.text === 'string') return value.text
    if (Array.isArray(value.parts)) {
      const lines = value.parts
        .map((part) => {
          if (!part || typeof part !== 'object') return ''
          const typed = part as { text?: unknown; url?: unknown; emoji?: unknown }
          if (typeof typed.text === 'string' && typed.text.trim()) return typed.text
          if (typeof typed.url === 'string' && typed.url.trim()) return typed.url
          if (typeof typed.emoji === 'string' && typed.emoji.trim()) return typed.emoji
          return ''
        })
        .filter(Boolean)
      if (lines.length) return lines.join('\n')
    }
  }
  return null
}

const extractTextFromEvent = (payload: string) => {
  try {
    const event = JSON.parse(payload)
    if (typeof event === 'string') return event
    if (typeof event?.text === 'string') return event.text
    const messageText = extractTextFromMessage(event?.message)
    if (messageText) return messageText
    if (typeof event?.delta === 'string') return event.delta
    if (typeof event?.delta?.content === 'string') return event.delta.content
    if (typeof event?.content === 'string') return event.content
    if (typeof event?.data === 'string') return event.data
    if (typeof event?.data?.text === 'string') return event.data.text
    if (typeof event?.data?.delta?.content === 'string') return event.data.delta.content
    const nestedMessageText = extractTextFromMessage(event?.data?.message)
    if (nestedMessageText) return nestedMessageText
    return null
  } catch {
    return payload
  }
}

// ---------------------------------------------------------------------------
// Stream chat
// ---------------------------------------------------------------------------

export const streamChat = async (query: string, botId: string, sessionId: string, token: TokenInfo) => {
  const config = readConfig()
  const baseURL = getBaseURL(config)
  const resp = await fetch(`${baseURL}/bots/${botId}/chat/stream?session_id=${encodeURIComponent(sessionId)}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token.access_token}`,
    },
    body: JSON.stringify({ query }),
  }).catch(() => null)
  if (!resp || !resp.ok || !resp.body) return false

  const stream = resp.body
  const reader = stream.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let printedText = false

  while (true) {
    const { value, done } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    let idx
    while ((idx = buffer.indexOf('\n')) >= 0) {
      const line = buffer.slice(0, idx).trim()
      buffer = buffer.slice(idx + 1)
      if (!line.startsWith('data:')) continue
      const payload = line.slice(5).trim()
      if (!payload || payload === '[DONE]') continue

      let event: Record<string, unknown>
      try {
        const parsed = JSON.parse(payload)
        if (typeof parsed === 'string') {
          process.stdout.write(parsed)
          printedText = true
          continue
        }
        event = parsed
      } catch {
        process.stdout.write(payload)
        printedText = true
        continue
      }

      const eventType = event.type as string | undefined

      switch (eventType) {
        case 'text_start':
          break

        case 'text_delta':
          if (typeof event.delta === 'string') {
            process.stdout.write(event.delta)
            printedText = true
          }
          break

        case 'text_end':
          if (printedText) {
            process.stdout.write('\n')
            printedText = false
          }
          break

        case 'tool_call_start': {
          if (printedText) {
            process.stdout.write('\n')
            printedText = false
          }
          const toolName = event.toolName as string
          const toolInput = event.input
          if (toolName === 'exec') {
            console.log(formatExecCall(toolInput))
          } else if (toolName === 'edit') {
            console.log(formatEditCall(toolInput))
          } else {
            const displayConfig = getToolDisplay(toolName)
            if (displayConfig.mode === 'expanded') {
              console.log(formatToolCallExpanded(displayConfig, toolName, toolInput))
            } else {
              console.log(formatToolCallInline(toolName, toolInput))
            }
          }
          break
        }

        case 'tool_call_end': {
          const toolName = event.toolName as string
          const result = event.result
          const resultLine = formatToolResult(toolName, result)
          if (resultLine) {
            console.log(resultLine)
          }
          break
        }

        case 'reasoning_start':
          if (printedText) {
            process.stdout.write('\n')
            printedText = false
          }
          process.stdout.write(chalk.dim('  💭 '))
          break

        case 'reasoning_delta':
          if (typeof event.delta === 'string') {
            process.stdout.write(chalk.dim(event.delta))
            printedText = true
          }
          break

        case 'reasoning_end':
          if (printedText) {
            process.stdout.write('\n')
            printedText = false
          }
          break

        case 'agent_start':
        case 'agent_end':
          break

        default: {
          const text = extractTextFromEvent(payload)
          if (text) {
            process.stdout.write(text)
            printedText = true
          }
          break
        }
      }
    }
  }
  if (printedText) {
    process.stdout.write('\n')
  }
  return true
}
