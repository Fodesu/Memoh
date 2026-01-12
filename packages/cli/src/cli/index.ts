#!/usr/bin/env bun

import { Command } from 'commander'
import chalk from 'chalk'
import { authCommands } from './commands/auth'
import { userCommands } from './commands/user'
import { modelCommands } from './commands/model'
import { platformCommands } from './commands/platform'
import { agentCommands, startInteractiveMode } from './commands/agent'
import { memoryCommands } from './commands/memory'
import { configCommands } from './commands/config'
import { scheduleCommands } from './commands/schedule'
import { debugCommands } from './commands/debug'

const program = new Command()

program
  .name('memoh')
  .description(chalk.bold.blue('🏠 Memoh Agent'))
  .version('1.0.0')

// Authentication commands
const auth = program.command('auth').description('User authentication management')
authCommands(auth)

// User management commands
const user = program.command('user').description('User management (requires admin privileges)')
userCommands(user)

// Model management commands
const model = program.command('model').description('AI model configuration management')
modelCommands(model)

// Platform management commands
const platform = program.command('platform').description('Platform configuration management')
platformCommands(platform)

// Agent conversation commands
const agent = program.command('agent').description('Chat with AI Agent')
agentCommands(agent)

// Memory management commands
const memory = program.command('memory').description('Memory management')
memoryCommands(memory)

// Config management commands
const config = program.command('config').description('User configuration management')
configCommands(config)

// Schedule management commands
const schedule = program.command('schedule').description('Schedule management')
scheduleCommands(schedule)

// Debug commands
const debug = program.command('debug').description('Debug tools')
debugCommands(debug)

// If no arguments provided, start interactive mode
if (process.argv.length === 2) {
  startInteractiveMode().catch((error) => {
    console.error('Failed to start interactive mode:', error)
    process.exit(1)
  })
} else {
  program.parse()
}

