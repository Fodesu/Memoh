import Elysia from 'elysia'
import { jwtPlugin } from '../../middlewares/auth'
import { LoginModel } from './model'
import { validateUser } from './service'

export const authModule = new Elysia({
  prefix: '/auth',
})
  .use(jwtPlugin)
  // Login endpoint
  .post('/login', async ({ body, jwt, set }) => {
    try {
      const user = await validateUser(body.username, body.password)
      
      if (!user) {
        set.status = 401
        return {
          success: false,
          error: 'Invalid username or password',
        }
      }

      // 使用 JWT 插件生成 token
      const token = await jwt.sign({
        userId: user.id,
        username: user.username,
        role: user.role,
      })

      return {
        success: true,
        data: {
          token,
          user: {
            id: user.id,
            username: user.username,
            role: user.role,
            displayName: user.displayName,
            email: user.email,
          },
        },
      }
    } catch (error) {
      set.status = 500
      return {
        success: false,
        error: error instanceof Error ? error.message : 'Failed to login',
      }
    }
  }, LoginModel)
  // Verify token endpoint
  .get('/verify', async ({ bearer, jwt, set }) => {
    try {
      if (!bearer) {
        set.status = 401
        return {
          success: false,
          error: 'No bearer token provided',
        }
      }

      // 使用 JWT 插件验证 token
      const payload = await jwt.verify(bearer)

      if (!payload) {
        set.status = 401
        return {
          success: false,
          error: 'Invalid or expired token',
        }
      }

      return {
        success: true,
        data: {
          userId: payload.userId,
          username: payload.username,
          role: payload.role,
        },
      }
    } catch {
      set.status = 401
      return {
        success: false,
        error: 'Invalid or expired token',
      }
    }
  })
  // Get current user info
  .get('/me', async ({ bearer, jwt, set }) => {
    try {
      if (!bearer) {
        set.status = 401
        return {
          success: false,
          error: 'No bearer token provided',
        }
      }

      // 使用 JWT 插件验证 token
      const payload = await jwt.verify(bearer)

      if (!payload) {
        set.status = 401
        return {
          success: false,
          error: 'Invalid or expired token',
        }
      }

      return {
        success: true,
        data: {
          userId: payload.userId,
          username: payload.username,
          role: payload.role,
        },
      }
    } catch {
      set.status = 401
      return {
        success: false,
        error: 'Invalid or expired token',
      }
    }
  })

