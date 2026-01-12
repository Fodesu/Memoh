import { Elysia } from 'elysia'
import { cors } from '@elysiajs/cors'
import { z } from 'zod'

export const SendSchema = z.object({
  message: z.string(),
  userId: z.string(),
})

export class BasePlatform {
  name: string = 'base'
  description: string = 'Base platform'
  started: boolean = false
  port: number = 7003

  config = z.record(z.string(), z.unknown())

  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  async start(config: z.infer<typeof this.config>): Promise<void> {}

  async stop(): Promise<void> {}

  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  async send(data: z.infer<typeof SendSchema>): Promise<void> {}

  serve<T extends z.infer<typeof this.config>>(config?: T): void {
    new Elysia()
      .use(cors({
        origin: '*',
        methods: ['GET', 'POST', 'PUT', 'DELETE', 'OPTIONS'],
        allowedHeaders: ['Content-Type', 'Authorization'],
        credentials: true,
      }))
      .onStart(() => {
        if (config) {
          this.started = true
          this.start(config)
        }
      })
      .post('/start', async ({ body }) => {
        if (!this.started) {
          this.start(body)
          this.started = true
        }
        return {
          success: true,
        }
      }, {
        body: this.config,
      })
      .post('/stop', async () => {
        if (this.started) {
          this.stop()
          this.started = false
        }
        return {
          success: true,
        }
      })
      .post('/send', async ({ body }) => {
        await this.send(body)
        return {
          success: true,
        }
      }, {
        body: SendSchema,
      })
      .listen(this.port)
  }
}