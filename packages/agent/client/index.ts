import { createInterface } from 'node:readline'
import { stdin as input, stdout as output } from 'node:process'
import { createAgent } from '../src/agent'
import { createMemory, filterByTimestamp, MemoryUnit } from '@memoh/memory'
import { ModelClientType, ChatModel, EmbeddingModel } from '@memoh/shared'

// Load environment variables
const MODEL = process.env.MODEL
const BASE_URL = process.env.BASE_URL
const API_KEY = process.env.API_KEY
const MODEL_CLIENT_TYPE = process.env.MODEL_CLIENT_TYPE || 'openai'

const EMBEDDING_MODEL = process.env.EMBEDDING_MODEL
const EMBEDDING_BASE_URL = process.env.EMBEDDING_BASE_URL || BASE_URL
const EMBEDDING_API_KEY = process.env.EMBEDDING_API_KEY || API_KEY
const EMBEDDING_CLIENT_TYPE = process.env.EMBEDDING_CLIENT_TYPE || 'openai'
const EMBEDDING_DIMENSIONS = parseInt(process.env.EMBEDDING_DIMENSIONS || '1536', 10)

const SUMMARY_MODEL = process.env.SUMMARY_MODEL || MODEL
const SUMMARY_BASE_URL = process.env.SUMMARY_BASE_URL || BASE_URL
const SUMMARY_API_KEY = process.env.SUMMARY_API_KEY || API_KEY
const SUMMARY_CLIENT_TYPE = process.env.SUMMARY_CLIENT_TYPE || MODEL_CLIENT_TYPE

if (!MODEL || !BASE_URL || !API_KEY || !EMBEDDING_MODEL) {
  console.error('Error: Missing required environment variables')
  console.error('Required: MODEL, BASE_URL, API_KEY, EMBEDDING_MODEL')
  console.error('Optional: MODEL_CLIENT_TYPE (default: openai)')
  console.error('Optional: SUMMARY_MODEL (default: same as MODEL)')
  console.error('Optional: SUMMARY_BASE_URL (default: same as BASE_URL)')
  console.error('Optional: SUMMARY_API_KEY (default: same as API_KEY)')
  console.error('Optional: SUMMARY_CLIENT_TYPE (default: same as MODEL_CLIENT_TYPE)')
  process.exit(1)
}

const USER_ID = 'cli-user'

// Create model configurations
const embeddingModel: EmbeddingModel = {
  modelId: EMBEDDING_MODEL!,
  baseUrl: EMBEDDING_BASE_URL!,
  apiKey: EMBEDDING_API_KEY!,
  clientType: EMBEDDING_CLIENT_TYPE as ModelClientType,
  dimensions: EMBEDDING_DIMENSIONS,
  name: `Embedding: ${EMBEDDING_MODEL}`,
}

const summaryModel: ChatModel = {
  modelId: SUMMARY_MODEL!,
  baseUrl: SUMMARY_BASE_URL!,
  apiKey: SUMMARY_API_KEY!,
  clientType: SUMMARY_CLIENT_TYPE as ModelClientType,
  name: `Summary: ${SUMMARY_MODEL}`,
}

// Create memory instance
const memoryInstance = createMemory({
  summaryModel,
  embeddingModel,
})

// Create agent
const agent = createAgent({
  model: {
    modelId: MODEL,
    baseUrl: BASE_URL,
    apiKey: API_KEY,
    clientType: MODEL_CLIENT_TYPE as ModelClientType,
    name: MODEL,
  },
  maxContextLoadTime: 60, // 60 minutes
  language: 'Same as user input',
  onReadMemory: async (from: Date, to: Date) => {
    return await filterByTimestamp(from, to, USER_ID)
  },
  onSearchMemory: async (query: string) => {
    const results = await memoryInstance.searchMemory(query, USER_ID)
    // Transform search results to MemoryUnit format
    // Note: mem0ai returns semantic search results, not full conversation history
    return results
  },
  onFinish: async (messages) => {
    // Save conversation to memory
    const memoryUnit: MemoryUnit = {
      messages: messages as unknown as MemoryUnit['messages'],
      timestamp: new Date(),
      user: USER_ID,
    }
    await memoryInstance.addMemory(memoryUnit)
  },
})

async function main() {
  console.log('🤖 Agent CLI Started')
  console.log('Type your message and press Enter. Type "exit" to quit.\n')

  // Load context
  // await agent.loadContext()

  const rl = createInterface({ input, output })

  rl.on('line', async (line) => {
    const userInput = line.trim()

    if (userInput === 'exit' || userInput === 'quit') {
      console.log('\n👋 Goodbye!')
      rl.close()
      process.exit(0)
    }

    if (!userInput) {
      rl.prompt()
      return
    }

    try {
      process.stdout.write('\n🤖 ')
      
      let hasOutput = false
      for await (const event of agent.ask(userInput)) {
        if (event.type === 'text-delta' && 'text' in event && event.text) {
          process.stdout.write(String(event.text))
          hasOutput = true
        } else if (event.type === 'tool-call' && 'toolName' in event) {
          process.stdout.write(`\n[Tool: ${event.toolName}]`)
          hasOutput = true
        }
      }

      if (!hasOutput) {
        process.stdout.write('(No response)')
      }
      console.log('\n')
    } catch (error) {
      console.error('\n❌ Error:', error instanceof Error ? error.message : String(error))
      console.log()
    }

    rl.prompt()
  })

  rl.setPrompt('You: ')
  rl.prompt()
}

main().catch(console.error)

