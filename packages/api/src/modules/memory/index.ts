import Elysia from 'elysia'
import { authMiddleware } from '../../middlewares/auth'
import { messageModule } from './message'
import { AddMemoryModel, SearchMemoryModel } from './model'
import { addMemory, searchMemory } from './service'
import { MemoryUnit } from '@memoh/memory'

export const memoryModule = new Elysia({
  prefix: '/memory',
})
  .use(authMiddleware)
  .use(messageModule)
  // Add memory for current user
  .post('/', async ({ user, body, set }) => {
    try {
      const memoryUnit: MemoryUnit = {
        ...body,
        user: user.userId,
      }
      const result = await addMemory(memoryUnit)
      return {
        success: true,
        data: result,
      }
    } catch (error) {
      set.status = 500
      return {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to add memory',
      }
    }
  }, AddMemoryModel)
  // Search memory for current user
  .get('/search', async ({ user, query, set }) => {
    try {
      const results = await searchMemory(query.query, user.userId)
      return {
        success: true,
        data: results,
      }
    } catch (error) {
      set.status = 500
      return {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to search memory',
      }
    }
  }, SearchMemoryModel)