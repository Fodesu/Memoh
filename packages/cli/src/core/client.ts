import { getContext, type MemohContext } from './context'
import { createClient as createClientApi } from '@memoh/api/client'

/**
 * Create API client
 * @param context - Optional context, uses global context if not provided
 */
export function createClient(context?: MemohContext) {
  const ctx = context || getContext()
  const storage = ctx.storage 


  const apiUrlResult = typeof storage.getApiUrl === 'function' 
    ? storage.getApiUrl() 
    : (storage as unknown as Record<string, string>).apiUrl

  
  if (apiUrlResult instanceof Promise) {
    throw new Error('createClient does not support async storage. Use createClientAsync instead.')
  }
  
  const apiUrl = apiUrlResult as string
  
  const token = typeof storage.getToken === 'function'
    ? storage.getToken(ctx.currentUserId)
    : null


  // Handle async token retrieval
  if (token instanceof Promise) {
    throw new Error('createClient does not support async token storage. Use createClientAsync instead.')
  }
  

  const client = createClientApi(apiUrl, token ?? undefined)


  return client
}


/**
 * Require authentication
 * Throws error if not authenticated
 * @param context - Optional context, uses global context if not provided
 */
export function requireAuth(context?: MemohContext): string {
  const ctx = context || getContext()
  const storage = ctx.storage
  
  const token = typeof storage.getToken === 'function'
    ? storage.getToken(ctx.currentUserId)
    : null

  if (token instanceof Promise) {
    throw new Error('requireAuth does not support async token storage. Use requireAuthAsync instead.')
  }

  if (!token) {
    throw new Error('Not logged in. Please login first')
  }
  
  return token
}

/**
 * Get API URL
 * @param context - Optional context, uses global context if not provided
 */
export function getApiUrl(context?: MemohContext): string {
  const ctx = context || getContext()
  const storage = ctx.storage
  
  const urlResult = typeof storage.getApiUrl === 'function'
    ? storage.getApiUrl()
    : (storage as unknown as Record<string, string>).apiUrl

  if (urlResult instanceof Promise) {
    throw new Error('getApiUrl does not support async storage. Use getApiUrlAsync instead.')
  }

  return urlResult as string
}

/**
 * Get token
 * @param context - Optional context, uses global context if not provided
 */
export function getToken(context?: MemohContext): string | null {
  const ctx = context || getContext()
  const storage = ctx.storage
  
  const token = typeof storage.getToken === 'function'
    ? storage.getToken(ctx.currentUserId)
    : null

  if (token instanceof Promise) {
    throw new Error('getToken does not support async storage. Use getTokenAsync instead.')
  }

  return token
}
