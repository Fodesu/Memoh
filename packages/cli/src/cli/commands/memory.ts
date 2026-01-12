import type { Command } from 'commander'
import chalk from 'chalk'
import ora from 'ora'
import * as memoryCore from '../../core/memory'
import { formatError } from '../../utils'

export function memoryCommands(program: Command) {
  program
    .command('search <query>')
    .description('Search memories')
    .option('-l, --limit <limit>', 'Number of results to return', '10')
    .action(async (query, options) => {
      try {
        const spinner = ora('Searching memories...').start()

        try {
          const results = await memoryCore.searchMemory({
            query,
            limit: parseInt(options.limit),
          })

          spinner.succeed(chalk.green(`Found ${results.length} memories`))

          if (results.length === 0) {
            console.log(chalk.yellow('No related memories found'))
            return
          }

          results.forEach((item, index) => {
            console.log()
            console.log(chalk.blue(`[${index + 1}] Similarity: ${((item.similarity || 0) * 100).toFixed(2)}%`))
            console.log(chalk.dim(`Time: ${new Date(item.timestamp).toLocaleString('en-US')}`))
            console.log(chalk.white(item.content))
          })
        } catch (error) {
          spinner.fail(chalk.red('Search failed'))
          console.error(chalk.red(formatError(error)))
          process.exit(1)
        }
      } catch (error: unknown) {
        const message = error instanceof Error ? error.message : String(error)
        console.error(chalk.red('Error:'), message)
        process.exit(1)
      }
    })

  program
    .command('add <content>')
    .description('Add memory')
    .action(async (content) => {
      try {
        const spinner = ora('Adding memory...').start()

        try {
          await memoryCore.addMemory({ content })
          spinner.succeed(chalk.green('Memory added'))
        } catch (error) {
          spinner.fail(chalk.red('Failed to add memory'))
          console.error(chalk.red(formatError(error)))
          process.exit(1)
        }
      } catch (error: unknown) {
        const message = error instanceof Error ? error.message : String(error)
        console.error(chalk.red('Error:'), message)
        process.exit(1)
      }
    })

  program
    .command('messages')
    .alias('msg')
    .description('Get message history')
    .option('-p, --page <page>', 'Page number', '1')
    .option('-l, --limit <limit>', 'Items per page', '20')
    .action(async (options) => {
      try {
        const spinner = ora('Fetching message history...').start()

        try {
          const result = await memoryCore.getMessages({
            page: parseInt(options.page),
            limit: parseInt(options.limit),
          })

          const { messages, pagination } = result
          spinner.succeed(chalk.green(`Message History (Page ${pagination.page}/${pagination.totalPages})`))

          if (messages.length === 0) {
            console.log(chalk.yellow('No messages'))
            return
          }

          console.log(chalk.dim(`\nTotal: ${pagination.total} messages\n`))

          messages.forEach((msg) => {
            const roleColor = msg.role === 'user' ? chalk.blue : chalk.green
            const roleIcon = msg.role === 'user' ? '👤' : '🤖'
            console.log(roleColor(`${roleIcon} ${msg.role.toUpperCase()}`))
            console.log(chalk.dim(new Date(msg.timestamp).toLocaleString('en-US')))
            console.log(chalk.white(msg.content))
            console.log()
          })
        } catch (error) {
          spinner.fail(chalk.red('Failed to fetch messages'))
          console.error(chalk.red(formatError(error)))
          process.exit(1)
        }
      } catch (error: unknown) {
        const message = error instanceof Error ? error.message : String(error)
        console.error(chalk.red('Error:'), message)
        process.exit(1)
      }
    })

  program
    .command('filter')
    .description('Filter messages by date range')
    .option('-s, --start <date>', 'Start date (ISO 8601)')
    .option('-e, --end <date>', 'End date (ISO 8601)')
    .action(async (options) => {
      try {
        if (!options.start || !options.end) {
          console.error(chalk.red('Please provide start and end dates'))
          console.log(chalk.dim('Example: memoh memory filter -s 2024-01-01T00:00:00Z -e 2024-12-31T23:59:59Z'))
          process.exit(1)
        }

        const spinner = ora('Filtering messages...').start()

        try {
          const messages = await memoryCore.filterMessages({
            startDate: options.start,
            endDate: options.end,
          })

          spinner.succeed(chalk.green(`Found ${messages.length} messages`))

          if (messages.length === 0) {
            console.log(chalk.yellow('No messages found'))
            return
          }

          console.log()

          messages.forEach((msg) => {
            const roleColor = msg.role === 'user' ? chalk.blue : chalk.green
            const roleIcon = msg.role === 'user' ? '👤' : '🤖'
            console.log(roleColor(`${roleIcon} ${msg.role.toUpperCase()}`))
            console.log(chalk.dim(new Date(msg.timestamp).toLocaleString('en-US')))
            console.log(chalk.white(msg.content))
            console.log()
          })
        } catch (error) {
          spinner.fail(chalk.red('Failed to filter messages'))
          console.error(chalk.red(formatError(error)))
          process.exit(1)
        }
      } catch (error: unknown) {
        const message = error instanceof Error ? error.message : String(error)
        console.error(chalk.red('Error:'), message)
        process.exit(1)
      }
    })
}

