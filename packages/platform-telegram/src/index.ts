import { Telegraf, type Context } from 'telegraf'
import { BasePlatform, SendSchema } from '@memoh/platform'
import { handleLogin, handleLogout, handleWhoami, requireAuth } from './auth'
import { chatStreamAsync, type StreamEvent } from '@memoh/client'
import { getTokenStorage } from './storage'
import z from 'zod'
import Redis from 'ioredis'

export interface TelegramPlatformConfig {
  botToken: string
  redisUrl?: string
  apiUrl?: string
}

export class TelegramPlatform extends BasePlatform {
  name = 'telegram'
  description = 'Telegram Bot platform for Memoh'
  config = z.object({
    botToken: z.string(),
  })
  port = 7101
  
  private bot?: Telegraf
  redis = new Redis(process.env.REDIS_URL || 'redis://localhost:6379')
  // private storage?: TelegramRedisStorage

  override async start(config: z.infer<typeof this.config>): Promise<void> {
    const botToken = config.botToken as string
    if (!botToken) {
      throw new Error('Bot token is required')
    }

    // // Initialize storage
    // this.storage = new TelegramRedisStorage({
    //   redisUrl: config.redisUrl as string,
    //   apiUrl: config.apiUrl as string,
    // })

    // Initialize bot
    this.bot = new Telegraf(botToken)

    // Register commands
    this.registerCommands()

    // Start bot
    this.bot.launch()
    console.log('✅ Telegram bot started successfully')
  }

  async stop(): Promise<void> {
    if (this.bot) {
      this.bot.stop('SIGTERM')
      console.log('🛑 Telegram bot stopped')
    }
    
    // if (this.storage) {
    //   await this.storage.close()
    //   console.log('🛑 Redis connection closed')
    // }
  }

  async send({ userId, message }: z.infer<typeof SendSchema>): Promise<void> {
    const pattern = 'memoh:telegram:*:userId'
      let cursor = '0'
      let telegramUserId: string | null = null
      
      do {
        const [nextCursor, keys] = await this.redis.scan(
          cursor,
          'MATCH',
          pattern,
          'COUNT',
          100
        )
        cursor = nextCursor
        
        // 检查每个 key 的值是否匹配目标 userId
        for (const key of keys) {
          const storedUserId = await this.redis.get(key)
          if (storedUserId === userId) {
            // 从 key 中提取 telegramUserId: memoh:telegram:{telegramUserId}:userId
            const match = key.match(/^memoh:telegram:(.+):userId$/)
            if (match) {
              telegramUserId = match[1]
              break
            }
          }
        }
      } while (cursor !== '0')
      if (telegramUserId) {
        const chatId = await this.redis.get(`memoh:telegram:${telegramUserId}:chatId`)
        if (chatId && this.bot) {
          await this.bot.telegram.sendMessage(chatId, message)
        }
      }
  }

  private registerCommands(): void {
    if (!this.bot) {
      throw new Error('Bot or storage not initialized')
    }

    // Start command
    this.bot.command('start', async (ctx) => {
      await ctx.reply(
        '👋 Welcome to Memoh Bot!\n\n' +
        'Available commands:\n' +
        '/login <username> <password> - Login to your account\n' +
        '/logout - Logout from your account\n' +
        '/whoami - Show current user info\n' +
        '/chat <message> - Chat with AI agent\n' +
        '/help - Show this help message'
      )
    })

    // Help command
    this.bot.command('help', async (ctx) => {
      await ctx.reply(
        '📚 Memoh Bot Help\n\n' +
        '🔐 Authentication:\n' +
        '/login <username> <password> - Login\n' +
        '/logout - Logout\n' +
        '/whoami - Show current user\n\n' +
        '💬 Chat:\n' +
        '/chat <message> - Talk to AI\n' +
        'Or just send a message directly\n\n' +
        '❓ Help:\n' +
        '/help - Show this message'
      )
    })

    // Auth commands
    this.bot.command('login', (ctx) => handleLogin(ctx))
    this.bot.command('logout', (ctx) => handleLogout(ctx))
    this.bot.command('whoami', (ctx) => handleWhoami(ctx))

    // Chat command (requires auth)
    this.bot.command('chat', requireAuth(), async (ctx) => {
      const args = ctx.message.text.split(' ').slice(1)
      if (args.length === 0) {
        await ctx.reply('❌ Please provide a message\n\nUsage: /chat <message>')
        return
      }

      const message = args.join(' ')
      await this.handleChat(ctx, message)
    })

    // Handle direct messages (requires auth)
    this.bot.on('text', requireAuth(), async (ctx) => {
      // Skip if it's a command
      if (ctx.message.text.startsWith('/')) {
        return
      }

      await this.handleChat(ctx, ctx.message.text)
    })

    // Error handling
    this.bot.catch((err, ctx) => {
      console.error('Bot error:', err)
      ctx.reply('❌ An error occurred. Please try again.')
    })
  }

  private async handleChat(ctx: Context, message: string): Promise<void> {
    try {      
      // Send typing indicator
      await ctx.sendChatAction('typing')
      await getTokenStorage(ctx)

      let responseText = ''
      let lastUpdateTime = Date.now()
      let messageId: number | undefined

      await chatStreamAsync(
        {
          message,
          language: 'Chinese',
          maxContextLoadTime: 60,
        },
        async (event: StreamEvent) => {
          if (event.type === 'text-delta' && event.text) {
            responseText += event.text

            // Update message every 1 second or when response is complete
            const now = Date.now()
            if (now - lastUpdateTime > 1000) {
              lastUpdateTime = now

              if (messageId && ctx.chat) {
                // Edit existing message
                try {
                  await ctx.telegram.editMessageText(
                    ctx.chat.id,
                    messageId,
                    undefined,
                    `🤖 ${responseText}`
                  )
                } catch {
                  // Ignore if message is not modified
                }
              } else {
                // Send first message
                const sent = await ctx.reply(`🤖 ${responseText}`)
                messageId = sent.message_id
              }
            }
          } else if (event.type === 'tool-call') {
            // Show tool usage
            if (messageId && ctx.chat) {
              try {
                await ctx.telegram.editMessageText(
                  ctx.chat.id,
                  messageId,
                  undefined,
                  `🤖 ${responseText}\n\n🔧 Using tool: ${event.toolName}...`
                )
              } catch {
                // Ignore
              }
            }
          } else if (event.type === 'error') {
            await ctx.reply(`❌ Error: ${event.error}`)
          } else if (event.type === 'done') {
            // Final update
            if (messageId && responseText && ctx.chat) {
              try {
                await ctx.telegram.editMessageText(
                  ctx.chat.id,
                  messageId,
                  undefined,
                  `🤖 ${responseText}`
                )
              } catch {
                // Ignore
              }
            } else if (!messageId && responseText) {
              await ctx.reply(`🤖 ${responseText}`)
            }
          }
        },
      )
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Unknown error'
      await ctx.reply(`❌ Error: ${errorMessage}`)
    }
  }
}

// Export for easy use
export { handleLogin, handleLogout, handleWhoami, requireAuth } from './auth'
