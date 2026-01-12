#!/usr/bin/env bun

/**
 * Example: Running Telegram Bot
 * 
 * This example shows how to start the Telegram bot programmatically
 */

import { TelegramPlatform } from './src/index'

async function main() {
  // Configuration
  const config = {
    botToken: process.env.BOT_TOKEN || '',
    redisUrl: process.env.REDIS_URL || 'redis://localhost:6379',
    apiUrl: process.env.API_BASE_URL || 'http://localhost:7002',
  }

  console.log('Starting Telegram bot with config:', {
    apiUrl: config.apiUrl,
    redisUrl: config.redisUrl,
    botToken: config.botToken.substring(0, 10) + '...',
  })

  // Create and start platform
  const platform = new TelegramPlatform()
  
  try {
    await platform.start(config)
    
    console.log('Bot is running!')
    console.log('\nAvailable commands:')
    console.log('  /start - Welcome message')
    console.log('  /login <username> <password> - Login to Memoh')
    console.log('  /whoami - Show current user')
    console.log('  /chat <message> - Chat with AI')
    console.log('  /logout - Logout')
    
    // Handle shutdown
    process.once('SIGINT', async () => {
      console.log('\nStopping bot...')
      await platform.stop()
      process.exit(0)
    })
    
  } catch (error) {
    console.error('Failed to start bot:', error)
    process.exit(1)
  }
}

// Only run if executed directly
if (import.meta.main) {
  main()
}

export { main }

