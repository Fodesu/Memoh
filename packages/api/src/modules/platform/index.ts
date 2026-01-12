import Elysia from 'elysia'
import { adminMiddleware, optionalAuthMiddleware } from '../../middlewares/auth'
import {
  CreatePlatformModel,
  UpdatePlatformModel,
  GetPlatformByIdModel,
  DeletePlatformModel,
  UpdatePlatformConfigModel,
  SetPlatformActiveModel,
} from './model'
import {
  getPlatforms,
  getPlatformById,
  createPlatform,
  updatePlatform,
  deletePlatform,
  updatePlatformConfig,
  getActivePlatforms,
  activePlatform,
  setActivePlatform,
} from './service'
import { Platform } from '@memoh/shared'

export const platformModule = new Elysia({
  prefix: '/platform',
})
  // 公开的读取接口 - 用户可读
  .use(optionalAuthMiddleware)
  // Get all platforms
  .onStart(async () => {
    const platforms = await getActivePlatforms()
    for (const platform of platforms) {
      await activePlatform({
        id: platform.id,
        name: platform.name,
        endpoint: platform.endpoint,
        config: platform.config as Record<string, unknown>,
        active: platform.active,
      })
    }
    console.log('platforms', platforms)
  })
  .get('/', async ({ query }) => {
    try {
      const page = parseInt(query.page as string) || 1
      const limit = parseInt(query.limit as string) || 10
      const sortOrder = (query.sortOrder as string) || 'desc'

      const result = await getPlatforms({
        page,
        limit,
        sortOrder: sortOrder as 'asc' | 'desc',
      })

      return {
        success: true,
        ...result,
      }
    } catch (error) {
      return {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to fetch platforms',
      }
    }
  })
  // Get platform by ID
  .get('/:id', async ({ params }) => {
    try {
      const { id } = params
      const platform = await getPlatformById(id)
      if (!platform) {
        return {
          success: false,
          error: 'Platform not found',
        }
      }
      return {
        success: true,
        data: platform,
      }
    } catch (error) {
      return {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to fetch platform',
      }
    }
  }, GetPlatformByIdModel)
  // 管理员权限的写入接口 - 管理员可读写
  .guard(
    {
      beforeHandle: () => {
        // This will be overridden by adminMiddleware
      },
    },
    (app) =>
      app
        .use(adminMiddleware)
        // Create new platform
        .post('/', async ({ body }) => {
          try {
            const newPlatform = await createPlatform(body as Omit<Platform, 'id'>)
            return {
              success: true,
              data: newPlatform,
            }
          } catch (error) {
            return {
              success: false,
              error: error instanceof Error ? error.message : 'Failed to create platform',
            }
          }
        }, CreatePlatformModel)
        // Update platform
        .put('/:id', async ({ params, body }) => {
          try {
            const { id } = params
            const updatedPlatform = await updatePlatform(id, body as Partial<Omit<Platform, 'id'>>)
            if (!updatedPlatform) {
              return {
                success: false,
                error: 'Platform not found',
              }
            }
            return {
              success: true,
              data: updatedPlatform,
            }
          } catch (error) {
            return {
              success: false,
              error: error instanceof Error ? error.message : 'Failed to update platform',
            }
          }
        }, UpdatePlatformModel)
        // Update platform config
        .put('/:id/config', async ({ params, body }) => {
          try {
            const { id } = params
            const { config } = body as { config: Record<string, unknown> }
            const updatedPlatform = await updatePlatformConfig(id, config)
            if (!updatedPlatform) {
              return {
                success: false,
                error: 'Platform not found',
              }
            }
            return {
              success: true,
              data: updatedPlatform,
            }
          } catch (error) {
            return {
              success: false,
              error: error instanceof Error ? error.message : 'Failed to update platform config',
            }
          }
        }, UpdatePlatformConfigModel)
        // Delete platform
        .delete('/:id', async ({ params }) => {
          try {
            const { id } = params
            const deletedPlatform = await deletePlatform(id)
            if (!deletedPlatform) {
              return {
                success: false,
                error: 'Platform not found',
              }
            }
            return {
              success: true,
              data: deletedPlatform,
            }
          } catch (error) {
            return {
              success: false,
              error: error instanceof Error ? error.message : 'Failed to delete platform',
            }
          }
        }, DeletePlatformModel)
        // Active platform
        .post('/:id/active', async ({ params }) => {
          try {
            const { id } = params
            const activatedPlatform = await setActivePlatform(id, true)
            if (!activatedPlatform) {
              return {
                success: false,
                error: 'Platform not found',
              }
            }
            return {
              success: true,
              data: activatedPlatform,
            }
          } catch (error) {
            return {
              success: false,
              error: error instanceof Error ? error.message : 'Failed to activate platform',
            }
          }
        }, SetPlatformActiveModel)
        // Inactive platform
        .post('/:id/inactive', async ({ params }) => {
          try {
            const { id } = params
            const inactivatedPlatform = await setActivePlatform(id, false)
            if (!inactivatedPlatform) {
              return {
                success: false,
                error: 'Platform not found',
              }
            }
            return {
              success: true,
              data: inactivatedPlatform,
            }
          } catch (error) {
            return {
              success: false,
              error: error instanceof Error ? error.message : 'Failed to inactivate platform',
            }
          }
        }, SetPlatformActiveModel)
  )
