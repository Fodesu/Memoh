import { Elysia } from 'elysia'
import { corsMiddleware, errorMiddleware } from './middlewares'
import { agentModule, authModule, modelModule, settingsModule, userModule } from './modules'
import { memoryModule } from './modules/memory'
import openapi from '@elysiajs/openapi'

const port = process.env.API_SERVER_PORT || 7002

export const app = new Elysia()
  .use(errorMiddleware)
  .use(openapi())
  .use(corsMiddleware)
  .use(authModule)
  .use(agentModule)
  .use(memoryModule)
  .use(modelModule)
  .use(settingsModule)
  .use(userModule)
  .listen(port)

console.log(
  `🦊 Elysia is running at ${app.server?.hostname}:${app.server?.port}`
)
