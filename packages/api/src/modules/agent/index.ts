import Elysia from 'elysia'
import { authMiddleware } from '../../middlewares/auth'
import { AgentStreamModel } from './model'
import { createAgentStream } from './service'
import { getChatModel, getEmbeddingModel, getSummaryModel } from '../model/service'
import { getSettings } from '../settings/service'
import { ChatModel, EmbeddingModel } from '@memohome/shared'

export const agentModule = new Elysia({
  prefix: '/agent',
})
  .use(authMiddleware)
  // Stream agent conversation
  .post('/stream', async ({ user, body, set }) => {
    try {
      // Get user's model configurations and settings
      const [chatModel, embeddingModel, summaryModel, userSettings] = await Promise.all([
        getChatModel(user.userId),
        getEmbeddingModel(user.userId),
        getSummaryModel(user.userId),
        getSettings(user.userId),
      ])

      if (!chatModel || !embeddingModel || !summaryModel) {
        set.status = 400
        return {
          success: false,
          error: 'Model configuration not found. Please configure your models in settings.',
        }
      }

      // Use body params if provided, otherwise use settings, otherwise use defaults
      const maxContextLoadTime = body.maxContextLoadTime 
        ?? userSettings?.maxContextLoadTime 
        ?? 60
      const language = body.language 
        ?? userSettings?.language 
        ?? 'Same as user input'

      // Create agent
      const agent = await createAgentStream({
        userId: user.userId,
        chatModel: chatModel.model as ChatModel,
        embeddingModel: embeddingModel.model as EmbeddingModel,
        summaryModel: summaryModel.model as ChatModel,
        maxContextLoadTime,
        language,
      })

      // Set headers for Server-Sent Events
      set.headers['Content-Type'] = 'text/event-stream'
      set.headers['Cache-Control'] = 'no-cache'
      set.headers['Connection'] = 'keep-alive'

      // Create a stream
      const stream = new ReadableStream({
        async start(controller) {
          try {
            const encoder = new TextEncoder()
            
            // Send events as they come
            for await (const event of agent.ask(body.message)) {
              const data = JSON.stringify(event)
              controller.enqueue(encoder.encode(`data: ${data}\n\n`))
            }

            // Send done event
            controller.enqueue(encoder.encode('data: [DONE]\n\n'))
            controller.close()
          } catch (error) {
            const errorMessage = error instanceof Error ? error.message : 'Unknown error'
            const errorData = JSON.stringify({ 
              type: 'error', 
              error: errorMessage 
            })
            controller.enqueue(new TextEncoder().encode(`data: ${errorData}\n\n`))
            controller.close()
          }
        },
      })

      return new Response(stream, {
        headers: {
          'Content-Type': 'text/event-stream',
          'Cache-Control': 'no-cache',
          'Connection': 'keep-alive',
        },
      })
    } catch (error) {
      set.status = 500
      return {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to process request',
      }
    }
  }, AgentStreamModel)