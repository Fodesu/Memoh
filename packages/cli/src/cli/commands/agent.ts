import type { Command } from 'commander'
import chalk from 'chalk'
import * as agentCore from '../../core/agent'
import { requireAuth } from '../../core/client'

export async function startInteractiveMode(options: { maxContextTime?: string; language?: string } = {}) {
  try {
    requireAuth()

    console.log(chalk.green.bold('🤖 Memoh Agent Interactive Mode'))
    console.log(chalk.dim('Type /exit or /quit to exit, type /help for help\n'))

    const { createInterface } = await import('readline')
    const rl = createInterface({
      input: process.stdin,
      output: process.stdout,
      prompt: chalk.blue('You: '),
    })

    rl.prompt()

    rl.on('line', async (line: string) => {
      const input = line.trim()

      if (input === '/exit' || input === '/quit') {
        console.log(chalk.yellow('Goodbye! 👋'))
        rl.close()
        process.exit(0)
        return
      }

      if (input === '/help') {
        console.log(chalk.green('\nAvailable commands:'))
        console.log(chalk.dim('  /exit, /quit - Exit interactive mode'))
        console.log(chalk.dim('  /help - Show help information\n'))
        rl.prompt()
        return
      }

      if (!input) {
        rl.prompt()
        return
      }

      try {
        console.log(chalk.green('Agent: '))

        await agentCore.chatStream(
          {
            message: input,
            maxContextLoadTime: parseInt(options.maxContextTime || '60'),
            language: options.language || 'Chinese',
          },
          async (event) => {
            if (event.type === 'text-delta' && event.text) {
              process.stdout.write(event.text)
            } else if (event.type === 'tool-call') {
              console.log(chalk.dim(`\n[🔧 ${event.toolName}]`))
            } else if (event.type === 'error') {
              console.error(chalk.red('\n❌'), event.error)
            } else if (event.type === 'done') {
              console.log('\n')
              rl.prompt()
            }
          }
        )
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error)
        console.error(chalk.red('Error:'), message)
        rl.prompt()
      }
    })

    rl.on('close', () => {
      console.log(chalk.yellow('\nGoodbye! 👋'))
      process.exit(0)
    })
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error)
    console.error(chalk.red('Error:'), message)
    process.exit(1)
  }
}

export function agentCommands(program: Command) {
  program
    .command('chat <message>')
    .description('Chat with AI Agent')
    .option('-t, --max-context-time <minutes>', 'Context load time (minutes)', '60')
    .option('-l, --language <language>', 'Response language', 'Chinese')
    .action(async (message, options) => {
      try {
        requireAuth()
        console.log(chalk.blue('🤖 Agent: '))

        await agentCore.chatStream(
          {
            message,
            maxContextLoadTime: parseInt(options.maxContextTime),
            language: options.language,
          },
          async (event) => {
            if (event.type === 'text-delta' && event.text) {
              process.stdout.write(event.text)
            } else if (event.type === 'tool-call') {
              console.log(chalk.dim(`\n[🔧 Using tool: ${event.toolName}]`))
            } else if (event.type === 'error') {
              console.error(chalk.red('\n❌ Error:'), event.error)
            } else if (event.type === 'done') {
              console.log('\n')
            }
          }
        )
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error)
        console.error(chalk.red('Error:'), message)
        process.exit(1)
      }
    })

  program
    .command('interactive')
    .alias('i')
    .description('Enter interactive conversation mode')
    .option('-t, --max-context-time <minutes>', 'Context load time (minutes)', '60')
    .option('-l, --language <language>', 'Response language', 'Chinese')
    .action(async (options) => {
      await startInteractiveMode(options)
    })
}

