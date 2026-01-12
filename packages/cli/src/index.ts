/**
 * Memoh CLI Package
 * 
 * This package provides both:
 * 1. A command-line interface (CLI) for interacting with Memoh API
 * 2. Core functionality that can be imported and used in other projects
 * 
 * @example CLI Usage (from terminal)
 * ```bash
 * memoh auth login
 * memoh agent chat "Hello"
 * ```
 * 
 * @example Core API Usage (from code)
 * ```typescript
 * import { login, chat, listModels } from '@memoh/cli'
 * 
 * // Login
 * await login({ username: 'admin', password: 'password' })
 * 
 * // Chat with agent
 * const response = await chat({ message: 'Hello' })
 * 
 * // List models
 * const models = await listModels()
 * ```
 */

// Export all core functionality
export * from './core'

// Export types
export * from './types'

// Export utilities
export * from './utils'
