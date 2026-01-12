# Memoh Telegram Platform

Telegram bot platform for Memoh, supporting multi-user authentication with Redis storage.

## Features

- 🔐 **Multi-user Authentication**: Each Telegram user can login to their own Memoh account
- 💾 **Redis Storage**: Token and user info stored in Redis
- 💬 **AI Chat**: Stream responses from Memoh AI agent
- 🔄 **Real-time Updates**: Live message editing during streaming responses
- 🛡️ **Auth Middleware**: Protected commands require login

## Installation

```bash
cd packages/platform-telegram
pnpm install
```

## Configuration

1. Create a `.env` file from the example:

```bash
cp .env.example .env
```

2. Configure your environment variables:

```env
# Get your bot token from @BotFather on Telegram
BOT_TOKEN=your_telegram_bot_token_here

# Redis connection string
REDIS_URL=redis://localhost:6379

# Memoh API URL
API_BASE_URL=http://localhost:7002
```

## Usage

### Standalone Bot

Run the bot as a standalone process:

```bash
pnpm start
```

Or in development mode with auto-reload:

```bash
pnpm dev
```

### As a Platform Module

```typescript
import { TelegramPlatform } from '@memoh/platform-telegram'

const platform = new TelegramPlatform()

await platform.start({
  botToken: process.env.BOT_TOKEN,
  redisUrl: process.env.REDIS_URL,
  apiUrl: process.env.API_BASE_URL,
})
```

## Bot Commands

### Authentication

- `/start` - Welcome message and command list
- `/login <username> <password>` - Login to your Memoh account
- `/logout` - Logout from your account
- `/whoami` - Show current user information

### Chat

- `/chat <message>` - Send a message to the AI agent
- Or just send a message directly (requires login)

### Help

- `/help` - Show all available commands

## Architecture

### Storage Structure

Redis keys follow this pattern:

```
memoh:tg:token:{telegram_user_id} -> token (30 days TTL)
memoh:tg:user:{telegram_user_id} -> { username, role, userId } (30 days TTL)
```

### Multi-user Support

Each Telegram user ID is mapped to their own Memoh account token:

```typescript
// User 123456 logs in
telegram_user_id: "123456" -> token: "abc..."
telegram_user_id: "123456" -> userInfo: { username: "user1", ... }

// User 789012 logs in
telegram_user_id: "789012" -> token: "def..."
telegram_user_id: "789012" -> userInfo: { username: "user2", ... }
```

### Authentication Flow

1. User sends `/login username password`
2. Bot validates credentials with Memoh API
3. Token is stored in Redis with Telegram user ID as key
4. User info is cached in Redis
5. Subsequent messages use the stored token

### Middleware

The `requireAuth` middleware checks if the user is logged in before allowing access to protected commands:

```typescript
bot.command('chat', requireAuth(storage), async (ctx) => {
  // User is guaranteed to be logged in
})
```

## Development

### Custom Commands

Add new commands to `src/index.ts`:

```typescript
this.bot.command('mycommand', requireAuth(storage), async (ctx) => {
  const memoContext = getMemoContext(ctx, storage)
  // Your command logic here
})
```

### Using Core Functions

```typescript
import { chatStreamAsync, listModels } from '@memoh/cli/core'
import { getMemoContext } from './auth'

// In a command handler
const memoContext = getMemoContext(ctx, storage)

// Chat with AI
await chatStreamAsync({
  message: 'Hello',
  language: 'Chinese'
}, async (event) => {
  if (event.type === 'text-delta') {
    // Handle streaming text
  }
}, memoContext)

// Or use other core functions
const models = await listModels()
```

## Testing

### Test Login

```
/login admin password
```

### Test Chat

```
/chat 你好，今天天气怎么样？
```

Or send a message directly:

```
介绍一下 TypeScript
```

## Troubleshooting

### Bot not responding

1. Check if the bot token is correct
2. Ensure Memoh API is running
3. Check Redis connection

### Login failed

1. Verify the API URL is correct
2. Check if the username/password is valid
3. Ensure the API server is accessible

### Redis errors

1. Make sure Redis is running: `redis-server`
2. Check the Redis URL in `.env`
3. Test connection: `redis-cli ping`

## Security Notes

- Never commit your `.env` file or bot token
- Tokens are stored with 30-day expiration
- Use HTTPS for production API endpoints
- Consider rate limiting for production use

## Production Deployment

### Docker

```dockerfile
FROM oven/bun:latest

WORKDIR /app
COPY package.json ./
RUN bun install

COPY . .

CMD ["bun", "run", "src/bot.ts"]
```

### Environment Variables

Set these in your deployment environment:

- `BOT_TOKEN` - Your Telegram bot token
- `REDIS_URL` - Redis connection string
- `API_BASE_URL` - Memoh API URL

## License

ISC
