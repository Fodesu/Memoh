import { Elysia } from 'elysia'

/**
 * 统一错误响应格式
 */
export interface ErrorResponse {
  success: false
  error: string
  code?: string
  details?: unknown
}

/**
 * 统一成功响应格式
 */
export interface SuccessResponse<T = unknown> {
  success: true
  data: T
  message?: string
}

/**
 * 统一错误处理中间件
 * 捕获所有未处理的错误并返回统一格式
 */
export const errorMiddleware = new Elysia({ name: 'error' })
  .onError(({ code, error, set }) => {
    console.error('[Error]', code, error)

    // 根据不同的错误类型设置不同的状态码和响应
    switch (code) {
      case 'VALIDATION':
        set.status = 400
        return {
          success: false,
          error: 'Validation failed',
          code: 'VALIDATION_ERROR',
          details: error.message,
        } satisfies ErrorResponse

      case 'NOT_FOUND':
        set.status = 404
        return {
          success: false,
          error: 'Resource not found',
          code: 'NOT_FOUND',
        } satisfies ErrorResponse

      case 'PARSE':
        set.status = 400
        return {
          success: false,
          error: 'Invalid request format',
          code: 'PARSE_ERROR',
          details: error.message,
        } satisfies ErrorResponse

      case 'INTERNAL_SERVER_ERROR':
        set.status = 500
        return {
          success: false,
          error: 'Internal server error',
          code: 'INTERNAL_SERVER_ERROR',
        } satisfies ErrorResponse

      case 'UNKNOWN':
      default:
        // 处理自定义错误
        if (error instanceof Error) {
          const message = error.message

          // 401 未授权错误
          if (
            message.includes('No bearer token') ||
            message.includes('Invalid or expired token')
          ) {
            set.status = 401
            return {
              success: false,
              error: message,
              code: 'UNAUTHORIZED',
            } satisfies ErrorResponse
          }

          // 403 权限不足错误
          if (message.includes('Forbidden') || message.includes('Admin access required')) {
            set.status = 403
            return {
              success: false,
              error: message,
              code: 'FORBIDDEN',
            } satisfies ErrorResponse
          }

          // 409 冲突错误（如用户已存在）
          if (message.includes('already exists')) {
            set.status = 409
            return {
              success: false,
              error: message,
              code: 'CONFLICT',
            } satisfies ErrorResponse
          }

          // 404 未找到错误
          if (message.includes('not found')) {
            set.status = 404
            return {
              success: false,
              error: message,
              code: 'NOT_FOUND',
            } satisfies ErrorResponse
          }

          // 默认 500 服务器错误
          set.status = 500
          return {
            success: false,
            error: message,
            code: 'ERROR',
          } satisfies ErrorResponse
        }

        // 未知错误
        set.status = 500
        return {
          success: false,
          error: 'An unexpected error occurred',
          code: 'UNKNOWN_ERROR',
        } satisfies ErrorResponse
    }
  })

