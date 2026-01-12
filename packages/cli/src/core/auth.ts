import { createClient } from './client'
import { getContext, type MemohContext } from './context'

export interface LoginParams {
  username: string
  password: string
}

export interface LoginResult {
  success: boolean
  token?: string
  user?: {
    username: string
    role: string
    id: string
  }
}

export interface UserInfo {
  username: string
  role: string
  id: string
}

export interface ConfigInfo {
  apiUrl: string
  loggedIn: boolean
}

/**
 * Login to Memoh API (sync version for file storage)
 * @param params - Login parameters
 * @param context - Optional context
 */
export async function login(params: LoginParams, context?: MemohContext): Promise<LoginResult> {
  const client = createClient(context)
  
  const response = await client.auth.login.post({
    username: params.username,
    password: params.password,
  })

  if (response.error) {
    throw new Error(response.error.value)
  }

  const data = response.data as { success?: boolean; data?: { token?: string; user?: { username: string; role: string; id: string } } } | null
  
  if (data?.success && data?.data?.token && data?.data?.user) {
    const ctx = context || getContext()
    const storage = ctx.storage
    
    // Set token (handle both sync and async)
    const setResult = storage.setToken(data.data.token, ctx.currentUserId)
    if (setResult instanceof Promise) {
      await setResult
    }
    
    return {
      success: true,
      token: data.data.token,
      user: data.data.user as UserInfo,
    }
  }
  
  throw new Error('Invalid response format')
}

/**
 * Logout current user
 * @param context - Optional context
 */
export function logout(context?: MemohContext): void {
  const ctx = context || getContext()
  const storage = ctx.storage
  
  const result = storage.clearToken(ctx.currentUserId)
  if (result instanceof Promise) {
    throw new Error('logout does not support async storage. Use logoutAsync instead.')
  }
}


/**
 * Check if user is logged in
 * @param context - Optional context
 */
export function isLoggedIn(context?: MemohContext): boolean {
  const ctx = context || getContext()
  const storage = ctx.storage
  
  const token = storage.getToken(ctx.currentUserId)
  
  if (token instanceof Promise) {
    throw new Error('isLoggedIn does not support async storage. Use isLoggedInAsync instead.')
  }
  
  return token !== null
}

/**
 * Get current logged in user info
 * @param context - Optional context
 */
export async function getCurrentUser(context?: MemohContext): Promise<UserInfo> {
  const ctx = context || getContext()
  const storage = ctx.storage
  
  const token = storage.getToken(ctx.currentUserId)
  
  if (token instanceof Promise) {
    throw new Error('getCurrentUser does not support async storage. Use getCurrentUserAsync instead.')
  }
  
  if (!token) {
    throw new Error('Not logged in')
  }

  const client = createClient(context)
  const response = await client.auth.me.get()

  if (response.error) {
    throw new Error(response.error.value)
  }

  const data = response.data as { success?: boolean; data?: UserInfo } | null
  
  if (data?.success && data?.data) {
    return data.data
  }
  
  throw new Error('Failed to fetch user information')
}

/**
 * Get current API configuration
 * @param context - Optional context
 */
export function getConfig(context?: MemohContext): ConfigInfo {
  const ctx = context || getContext()
  const storage = ctx.storage
  
  const apiUrl = storage.getApiUrl()
  const token = storage.getToken(ctx.currentUserId)
  
  if (apiUrl instanceof Promise || token instanceof Promise) {
    throw new Error('getConfig does not support async storage. Use getConfigAsync instead.')
  }
  
  return {
    apiUrl: apiUrl as string,
    loggedIn: token !== null,
  }
}

/**
 * Set API URL
 * @param url - API URL
 * @param context - Optional context
 */
export function setConfig(url: string, context?: MemohContext): void {
  const ctx = context || getContext()
  const storage = ctx.storage
  
  const result = storage.setApiUrl(url)
  if (result instanceof Promise) {
    throw new Error('setConfig does not support async storage. Use setConfigAsync instead.')
  }
}

// Re-export for backward compatibility
export { getToken, getApiUrl } from './client'
export { getContext, setContext, createContext } from './context'
