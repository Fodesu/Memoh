import { getApiUrl, getToken } from './client'
import type { MemohContext } from './context'

export interface PingResult {
  success: boolean
  status?: number
  message?: string
  error?: string
}

/**
 * Test API server connection
 * @param context - Optional context, uses global context if not provided
 */
export async function ping(context?: MemohContext): Promise<PingResult> {
  const apiUrl = getApiUrl(context)
  const token = getToken(context)
  
  try {
    const controller = new AbortController()
    const timeoutId = setTimeout(() => controller.abort(), 5000)
    
    const response = await fetch(`${apiUrl}/`, {
      signal: controller.signal,
      headers: token ? {
        'Authorization': `Bearer ${token}`
      } : {}
    })
    
    clearTimeout(timeoutId)
    
    if (response.ok) {
      const text = await response.text()
      return {
        success: true,
        status: response.status,
        message: text.substring(0, 100),
      }
    } else {
      return {
        success: false,
        status: response.status,
        error: `HTTP ${response.status}`,
      }
    }
  } catch (error) {
    if (error instanceof Error) {
      if (error.name === 'AbortError') {
        return {
          success: false,
          error: 'Connection timeout (5 seconds)',
        }
      }
      return {
        success: false,
        error: error.message,
      }
    }
    return {
      success: false,
      error: 'Unknown error',
    }
  }
}

/**
 * Get connection info
 * @param context - Optional context, uses global context if not provided
 */
export function getConnectionInfo(context?: MemohContext): {
  apiUrl: string
  hasToken: boolean
} {
  return {
    apiUrl: getApiUrl(context),
    hasToken: getToken(context) !== null,
  }
}

