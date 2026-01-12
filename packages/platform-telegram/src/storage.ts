import type { TokenStorage } from '@memoh/client'
import Redis from 'ioredis'
import { Context } from 'telegraf'

export interface TelegramTokenStorage extends TokenStorage {
  getUserId: () => string | null
  setUserId: (userId: string) => void
  getChatId: () => string | null
  setChatId: (chatId: string) => void
  getTelegramIdByUserId: (userId: string) => Promise<string | null>
}

export const getTokenStorage = async (ctx: Context): Promise<TelegramTokenStorage> => {
  const telegramUserId = ctx.from?.id.toString()
  if (!telegramUserId) {
    throw new Error('Unable to identify user')
  }
  const redis = new Redis(process.env.REDIS_URL || 'redis://localhost:6379')
  const isTokenExists = await redis.exists(`memoh:telegram:${telegramUserId}:token`)
  const token = isTokenExists ? await redis.get(`memoh:telegram:${telegramUserId}:token`) : null
  const isUserIdExists = await redis.exists(`memoh:telegram:${telegramUserId}:userId`)
  const userId = isUserIdExists ? await redis.get(`memoh:telegram:${telegramUserId}:userId`) : null
  const chatId = ctx.chat?.id.toString() ?? null
  if (chatId) await redis.set(`memoh:telegram:${telegramUserId}:chatId`, chatId)
  return {
    getChatId: () => chatId,
    setChatId: (chatId: string) => {
      redis.set(`memoh:telegram:${telegramUserId}:chatId`, chatId)
        .then(() => {
          redis.save()
        })
    },
    getApiUrl: () => process.env.API_URL || 'http://localhost:7002',
    setApiUrl: () => {},
    getToken: () => token,
    setToken: (token: string) => {
      redis.set(`memoh:telegram:${telegramUserId}:token`, token)
        .then(() => {
          redis.save()
        })
    },
    clearToken: () => {
      redis.del(`memoh:telegram:${telegramUserId}:token`)
        .then(() => {
          redis.save()
        })
    },
    getUserId: () => userId,
    setUserId: (userId: string) => {
      redis.set(`memoh:telegram:${telegramUserId}:userId`, userId)
        .then(() => {
          redis.save()
        })
    },
    getTelegramIdByUserId: async (userId: string) => {
      // 扫描所有 memoh:telegram:*:userId 的 key
      const pattern = 'memoh:telegram:*:userId'
      let cursor = '0'
      
      do {
        const [nextCursor, keys] = await redis.scan(
          cursor,
          'MATCH',
          pattern,
          'COUNT',
          100
        )
        cursor = nextCursor
        
        // 检查每个 key 的值是否匹配目标 userId
        for (const key of keys) {
          const storedUserId = await redis.get(key)
          if (storedUserId === userId) {
            // 从 key 中提取 telegramUserId: memoh:telegram:{telegramUserId}:userId
            const match = key.match(/^memoh:telegram:(.+):userId$/)
            if (match) {
              return match[1]
            }
          }
        }
      } while (cursor !== '0')
      
      return null
    },
  }
}

