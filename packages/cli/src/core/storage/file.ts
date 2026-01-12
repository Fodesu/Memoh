import { homedir } from 'os'
import { join } from 'path'
import { existsSync, readFileSync, writeFileSync, mkdirSync } from 'fs'
import type { TokenStorage, Config } from '../storage'

const CONFIG_DIR = join(homedir(), '.memoh')
const CONFIG_FILE = join(CONFIG_DIR, 'config.json')

const DEFAULT_CONFIG: Config = {
  apiUrl: process.env.API_BASE_URL || 'http://localhost:7002',
}

/**
 * File-based token storage for CLI
 * Stores config in ~/.memoh/config.json
 */
export class FileTokenStorage implements TokenStorage {
  private ensureConfigDir() {
    if (!existsSync(CONFIG_DIR)) {
      mkdirSync(CONFIG_DIR, { recursive: true })
    }
  }

  loadConfig(): Config {
    this.ensureConfigDir()
    
    if (!existsSync(CONFIG_FILE)) {
      this.saveConfig(DEFAULT_CONFIG)
      return DEFAULT_CONFIG
    }

    try {
      const data = readFileSync(CONFIG_FILE, 'utf-8')
      return { ...DEFAULT_CONFIG, ...JSON.parse(data) }
    } catch {
      return DEFAULT_CONFIG
    }
  }

  saveConfig(config: Config): void {
    this.ensureConfigDir()
    writeFileSync(CONFIG_FILE, JSON.stringify(config, null, 2))
  }

  getApiUrl(): string {
    const config = this.loadConfig()
    return config.apiUrl
  }

  setApiUrl(url: string): void {
    const config = this.loadConfig()
    config.apiUrl = url
    this.saveConfig(config)
  }

  getToken(): string | null {
    const config = this.loadConfig()
    return config.token || null
  }

  setToken(token: string): void {
    const config = this.loadConfig()
    config.token = token
    this.saveConfig(config)
  }

  clearToken(): void {
    const config = this.loadConfig()
    delete config.token
    this.saveConfig(config)
  }
}

