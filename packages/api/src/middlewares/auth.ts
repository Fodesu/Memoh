import { Elysia } from 'elysia'
import { bearer } from '@elysiajs/bearer'
import { jwt } from '@elysiajs/jwt'

/**
 * JWT 配置常量
 */
const JWT_CONFIG = {
  name: 'jwt',
  secret: process.env.JWT_SECRET || 'your-secret-key-change-in-production',
  exp: process.env.JWT_EXPIRES_IN || '7d',
}

/**
 * 用户信息类型
 */
export type AuthUser = {
  userId: string
  username: string
  role: string
}

/**
 * 共享的基础认证插件
 * 提供 JWT 和 Bearer token 功能
 */
export const jwtPlugin = new Elysia({ name: 'jwt-plugin' })
  .use(jwt(JWT_CONFIG))
  .use(bearer())

/**
 * 认证中间件
 * 验证 Bearer token 并将用户信息注入到 context 中
 */
export const authMiddleware = new Elysia({ name: 'auth' })
  .use(jwt(JWT_CONFIG))
  .use(bearer())
  .derive({ as: 'scoped' }, async ({ bearer, jwt, set }) => {
    if (!bearer) {
      set.status = 401
      throw new Error('No bearer token provided')
    }

    const payload = await jwt.verify(bearer)

    if (!payload) {
      set.status = 401
      throw new Error('Invalid or expired token')
    }

    return {
      user: {
        userId: payload.userId as string,
        username: payload.username as string,
        role: payload.role as string,
      } as AuthUser,
    }
  })

/**
 * 可选认证中间件
 * 如果有 token 则验证，没有 token 则继续（user 为 null）
 */
export const optionalAuthMiddleware = new Elysia({ name: 'optional-auth' })
  .use(jwt(JWT_CONFIG))
  .use(bearer())
  .derive({ as: 'scoped' }, async ({ bearer, jwt }) => {
    if (!bearer) {
      return { user: null as AuthUser | null }
    }

    const payload = await jwt.verify(bearer)

    if (!payload) {
      return { user: null as AuthUser | null }
    }

    return {
      user: {
        userId: payload.userId as string,
        username: payload.username as string,
        role: payload.role as string,
      } as AuthUser | null,
    }
  })

/**
 * 管理员权限中间件
 * 验证 token 并检查用户是否为管理员
 */
export const adminMiddleware = new Elysia({ name: 'admin' })
  .use(jwt(JWT_CONFIG))
  .use(bearer())
  .derive({ as: 'scoped' }, async ({ bearer, jwt, set }) => {
    if (!bearer) {
      set.status = 401
      throw new Error('No bearer token provided')
    }

    const payload = await jwt.verify(bearer)

    if (!payload) {
      set.status = 401
      throw new Error('Invalid or expired token')
    }

    const user: AuthUser = {
      userId: payload.userId as string,
      username: payload.username as string,
      role: payload.role as string,
    }

    // 检查是否为管理员
    if (user.role !== 'admin') {
      set.status = 403
      throw new Error('Forbidden: Admin access required')
    }

    return { user }
  })

