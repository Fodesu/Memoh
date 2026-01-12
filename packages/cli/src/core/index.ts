/**
 * Memoh Core API
 * 
 * This module provides core functionality that can be used by CLI and other applications.
 * All functions are independent of CLI-specific UI concerns (no chalk, ora, inquirer, etc.)
 */

// Context
export {
  getContext,
  setContext,
  createContext,
  resetContext,
  type MemohContext,
} from './context'

// Storage
export type { TokenStorage, Config } from './storage'
export { FileTokenStorage } from './storage/'

// Auth
export {
  login,
  logout,
  isLoggedIn,
  getCurrentUser,
  getConfig,
  setConfig,
  type LoginParams,
  type LoginResult,
  type UserInfo,
  type ConfigInfo,
} from './auth'

// User
export {
  listUsers,
  createUser,
  getUser,
  deleteUser,
  updateUserPassword,
  type CreateUserParams,
  type UpdatePasswordParams,
} from './user'

// Model
export {
  listModels,
  createModel,
  getModel,
  deleteModel,
  getDefaultModels,
  type CreateModelParams,
  type ModelListItem,
} from './model'

// Platform
export {
  listPlatforms,
  createPlatform,
  getPlatform,
  updatePlatform,
  updatePlatformConfig,
  deletePlatform,
  activatePlatform,
  inactivatePlatform,
  type CreatePlatformParams,
  type PlatformListItem,
} from './platform'

// Agent
export {
  chat,
  chatAsync,
  chatStream,
  chatStreamAsync,
  type ChatParams,
  type StreamEvent,
  type StreamCallback,
} from './agent'

// Memory
export {
  searchMemory,
  addMemory,
  getMessages,
  filterMessages,
  type SearchMemoryParams,
  type AddMemoryParams,
  type GetMessagesParams,
  type FilterMessagesParams,
} from './memory'

// Schedule
export {
  listSchedules,
  createSchedule,
  getSchedule,
  updateSchedule,
  deleteSchedule,
  toggleSchedule,
  type CreateScheduleParams,
  type UpdateScheduleParams,
} from './schedule'

// Settings
export {
  getSettings,
  updateSettings,
  type UpdateSettingsParams,
} from './settings'

// Debug
export {
  ping,
  getConnectionInfo,
  type PingResult,
} from './debug'

// Client
export {
  createClient,
  requireAuth,
  getApiUrl,
  getToken,
} from './client'
