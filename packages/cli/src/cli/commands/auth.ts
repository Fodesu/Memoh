import type { Command } from 'commander'
import chalk from 'chalk'
import inquirer from 'inquirer'
import ora from 'ora'
import * as authCore from '../../core/auth'
import { formatError } from '../../utils'

export function authCommands(program: Command) {
  program
    .command('login')
    .description('Login to Memoh')
    .option('-u, --username <username>', 'Username')
    .option('-p, --password <password>', 'Password')
    .action(async (options) => {
      try {
        let username = options.username
        let password = options.password

        if (!username || !password) {
          const answers = await inquirer.prompt([
            {
              type: 'input',
              name: 'username',
              message: 'Please enter username:',
              when: !username,
            },
            {
              type: 'password',
              name: 'password',
              message: 'Please enter password:',
              when: !password,
              mask: '*',
            },
          ])
          username = username || answers.username
          password = password || answers.password
        }

        const spinner = ora('Logging in...').start()

        try {
          const result = await authCore.login({ username, password })
          spinner.succeed(chalk.green('Login successful!'))
          console.log(chalk.blue(`User: ${result.user?.username}`))
          console.log(chalk.blue(`Role: ${result.user?.role}`))
        } catch (error) {
          spinner.fail(chalk.red('Login failed'))
          console.error(chalk.red(formatError(error)))
          process.exit(1)
        }
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error)
        console.error(chalk.red('Login error:'), message)
        process.exit(1)
      }
    })

  program
    .command('logout')
    .description('Logout current user')
    .action(() => {
      if (!authCore.isLoggedIn()) {
        console.log(chalk.yellow('Not currently logged in'))
        return
      }

      authCore.logout()
      console.log(chalk.green('✓ Logged out'))
    })

  program
    .command('whoami')
    .description('View current logged in user')
    .action(async () => {
      try {
        if (!authCore.isLoggedIn()) {
          console.log(chalk.yellow('Not currently logged in'))
          console.log(chalk.dim('Use "memoh auth login" to login'))
          return
        }

        const spinner = ora('Fetching user information...').start()

        try {
          const user = await authCore.getCurrentUser()
          spinner.succeed(chalk.green('Logged in'))
          console.log(chalk.blue(`Username: ${user.username}`))
          console.log(chalk.blue(`Role: ${user.role}`))
          console.log(chalk.blue(`User ID: ${user.id}`))
        } catch (error) {
          spinner.fail(chalk.red('Failed to fetch user information'))
          console.error(chalk.red(formatError(error)))
          process.exit(1)
        }
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error)
        console.error(chalk.red('Error:'), message)
        process.exit(1)
      }
    })

  program
    .command('config')
    .description('View or set API configuration')
    .option('-s, --set <url>', 'Set API URL')
    .action((options) => {
      if (options.set) {
        const url = options.set
        authCore.setConfig(url)
        console.log(chalk.green(`✓ API URL set to: ${url}`))
      } else {
        const config = authCore.getConfig()
        console.log(chalk.blue('Current configuration:'))
        console.log(chalk.dim(`API URL: ${config.apiUrl}`))
        console.log(chalk.dim(`Logged in: ${config.loggedIn ? 'Yes' : 'No'}`))
      }
    })
}

