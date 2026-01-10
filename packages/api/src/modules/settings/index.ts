import Elysia from 'elysia'
import { authMiddleware } from '../../middlewares/auth'
import { UpdateSettingsModel } from './model'
import { getSettings, upsertSettings } from './service'

export const settingsModule = new Elysia({
  prefix: '/settings',
})
  .use(authMiddleware)
  // Get current user's settings
  .get('/', async ({ user, set }) => {
    try {
      const userSettings = await getSettings(user.userId)
      if (!userSettings) {
        set.status = 404
        return {
          success: false,
          error: 'Settings not found',
        }
      }
      return {
        success: true,
        data: userSettings,
      }
    } catch (error) {
      set.status = 500
      return {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to fetch settings',
      }
    }
  })
  // Update or create current user's settings
  .put('/', async ({ user, body, set }) => {
    try {
      const result = await upsertSettings(user.userId, body)
      return {
        success: true,
        data: result,
      }
    } catch (error) {
      set.status = 500
      return {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to update settings',
      }
    }
  }, UpdateSettingsModel)

