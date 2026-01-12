/**
 * Memoh Core Context
 * 
 * Provides a configurable context for core functions to use different storage backends
 */

import type { TokenStorage } from './storage'
import { FileTokenStorage } from './storage/file'

/**
 * Global context for core functions
 */
export interface MemohContext {
  storage: TokenStorage
  currentUserId?: string
}

/**
 * Default context (uses file storage for CLI)
 */
let defaultContext: MemohContext = {
  storage: new FileTokenStorage(),
}

/**
 * Get the current context
 */
export function getContext(): MemohContext {
  return defaultContext
}

/**
 * Set the global context
 * Use this to configure storage backend (e.g., Redis for Telegram bot)
 */
export function setContext(context: Partial<MemohContext>): void {
  defaultContext = { ...defaultContext, ...context }
}

/**
 * Create a new context without modifying the global one
 * Useful for multi-user scenarios
 */
export function createContext(options: {
  storage: TokenStorage
  userId?: string
}): MemohContext {
  return {
    storage: options.storage,
    currentUserId: options.userId,
  }
}

/**
 * Reset context to default (file storage)
 */
export function resetContext(): void {
  defaultContext = {
    storage: new FileTokenStorage(),
  }
}

