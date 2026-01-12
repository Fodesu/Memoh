# Telegram Bot Setup Guide

## Prerequisites

1. **Telegram Bot Token**
   - Open Telegram and search for `@BotFather`
   - Send `/newbot` and follow instructions
   - Copy the bot token provided

2. **Redis Server**
   ```bash
   # Install Redis (macOS)
   brew install redis
   
   # Start Redis
   redis-server
   
   # Or with Docker
   docker run -d -p 6379:6379 redis:latest
   ```

3. **Memoh API**
   - Ensure the API server is running on `http://localhost:7002`
   - Or update `API_BASE_URL` to your API endpoint

## Quick Start

### 1. Create Environment File

Create `.env` file in `packages/platform-telegram/`:

```env
BOT_TOKEN=123456789:ABCdefGHIjklMNOpqrsTUVwxyz
REDIS_URL=redis://localhost:6379
API_BASE_URL=http://localhost:7002
```

### 2. Install Dependencies

```bash
cd packages/platform-telegram
pnpm install
```

### 3. Start the Bot

```bash
pnpm start
```

You should see:

```
🚀 Starting Telegram bot...
📡 API URL: http://localhost:7002
💾 Redis URL: redis://localhost:6379
✅ Telegram bot started successfully
✅ Bot is running...
Press Ctrl+C to stop
```

### 4. Test the Bot

Open Telegram and find your bot, then:

1. **Start conversation**
   ```
   /start
   ```

2. **Login to Memoh**
   ```
   /login admin password
   ```

3. **Check your user**
   ```
   /whoami
   ```

4. **Chat with AI**
   ```
   /chat 你好，介绍一下你自己
   ```
   
   Or just send a message directly:
   ```
   今天天气怎么样？
   ```

## Architecture Overview

### Storage Model

```
┌─────────────────────┐
│  Telegram User 1    │──► telegram_id: 123456
│  @user1             │    ├─ token: "abc..."
└─────────────────────┘    └─ userInfo: { username: "user1", ... }

┌─────────────────────┐
│  Telegram User 2    │──► telegram_id: 789012
│  @user2             │    ├─ token: "def..."
└─────────────────────┘    └─ userInfo: { username: "user2", ... }
```

### Data Flow

```
1. User: /login admin password
   │
   ├──► Bot validates with Memoh API
   │
   ├──► API returns token + user info
   │
   └──► Redis stores:
        ├─ memoh:tg:token:123456 = "token_abc..."
        └─ memoh:tg:user:123456 = { username, role, userId }

2. User: /chat Hello
   │
   ├──► Middleware checks: is logged in?
   │
   ├──► Get token from Redis by telegram_id
   │
   ├──► Call Memoh API with token
   │
   └──► Stream response back to user
```

## Common Use Cases

### Multiple Users

Each Telegram user can have their own account:

```
# User A logs in
@userA: /login adminA passwordA
Bot: ✅ Login successful! Username: adminA

# User B logs in
@userB: /login userB passwordB
Bot: ✅ Login successful! Username: userB

# Both can chat independently
@userA: /chat What's the weather?
@userB: /chat Tell me a joke
```

### Session Management

Tokens expire after 30 days. To re-login:

```
# Logout
/logout

# Login again
/login username password
```

### Check Authentication

```
/whoami

Response:
👤 Current User:

Username: admin
Role: admin
User ID: user-id-xxx
Telegram ID: 123456789
```

## Development

### Project Structure

```
packages/platform-telegram/
├── src/
│   ├── index.ts       # Main platform class
│   ├── bot.ts         # Standalone entry point
│   ├── auth.ts        # Auth handlers & middleware
│   └── storage.ts     # Redis storage implementation
├── package.json
└── README.md
```

### Adding Custom Commands

Edit `src/index.ts`:

```typescript
// Add a new command
this.bot.command('mycommand', requireAuth(storage), async (ctx) => {
  const memoContext = getMemoContext(ctx, storage)
  
  // Your logic here
  await ctx.reply('Command executed!')
})
```

### Using Core Functions

```typescript
import { 
  chatStreamAsync, 
  listModels, 
  searchMemory 
} from '@memoh/cli/core'

// In command handler
const memoContext = getMemoContext(ctx, storage)

// List models
const models = await listModels()

// Search memory
const memories = await searchMemory({ 
  query: 'TypeScript',
  limit: 5 
})

// Chat with streaming
await chatStreamAsync({
  message: 'Hello'
}, async (event) => {
  if (event.type === 'text-delta') {
    console.log(event.text)
  }
}, memoContext)
```

## Debugging

### Enable Verbose Logging

```bash
DEBUG=telegraf:* pnpm start
```

### Check Redis Data

```bash
# Connect to Redis CLI
redis-cli

# List all keys
KEYS memoh:tg:*

# Get a token
GET memoh:tg:token:123456

# Get user info
GET memoh:tg:user:123456

# Check TTL (time to live)
TTL memoh:tg:token:123456
```

### Test API Connection

```bash
# Test if API is accessible
curl http://localhost:7002/

# Test login endpoint
curl -X POST http://localhost:7002/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"password"}'
```

## Production Deployment

### Using Docker Compose

Create `docker-compose.yml`:

```yaml
version: '3.8'

services:
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    volumes:
      - redis-data:/data

  telegram-bot:
    build: .
    environment:
      - BOT_TOKEN=${BOT_TOKEN}
      - REDIS_URL=redis://redis:6379
      - API_BASE_URL=${API_BASE_URL}
    depends_on:
      - redis
    restart: unless-stopped

volumes:
  redis-data:
```

Start with:

```bash
docker-compose up -d
```

### Environment Variables for Production

```env
BOT_TOKEN=your_production_bot_token
REDIS_URL=redis://your-redis-host:6379
API_BASE_URL=https://api.yourdomain.com
```

### Security Considerations

1. **Never commit** `.env` files or bot tokens
2. **Use HTTPS** for production API endpoints
3. **Rate limiting**: Consider adding rate limits to prevent abuse
4. **Token rotation**: Implement token refresh mechanism
5. **Monitoring**: Add logging and error tracking (e.g., Sentry)

## Troubleshooting

### Bot not responding

**Issue**: Bot doesn't respond to commands

**Solutions**:
1. Check bot is running: `pnpm start`
2. Verify bot token is correct
3. Check if bot is blocked by user
4. Review logs for errors

### Authentication failures

**Issue**: `/login` fails

**Solutions**:
1. Verify API URL is accessible
2. Check username/password are correct
3. Ensure Memoh API is running
4. Test API endpoint directly with curl

### Redis connection errors

**Issue**: Cannot connect to Redis

**Solutions**:
1. Check Redis is running: `redis-cli ping` should return `PONG`
2. Verify REDIS_URL in `.env`
3. Check firewall settings
4. For Docker: ensure containers are on same network

### Token expiration

**Issue**: Commands fail after some time

**Solutions**:
1. Tokens expire after 30 days
2. User needs to `/logout` and `/login` again
3. Check token TTL in Redis: `TTL memoh:tg:token:123456`

## Advanced Usage

### Custom Storage Backend

You can implement your own storage:

```typescript
import type { TokenStorage } from '@memoh/cli/core'

class MyCustomStorage implements TokenStorage {
  async getApiUrl(): Promise<string> { /* ... */ }
  async setApiUrl(url: string): Promise<void> { /* ... */ }
  async getToken(userId?: string): Promise<string | null> { /* ... */ }
  async setToken(token: string, userId?: string): Promise<void> { /* ... */ }
  async clearToken(userId?: string): Promise<void> { /* ... */ }
}

// Use it
const storage = new MyCustomStorage()
const platform = new TelegramPlatform()
// ... modify platform to use custom storage
```

### Webhook Mode (instead of polling)

For production, use webhooks instead of long polling:

```typescript
import { Telegraf } from 'telegraf'

const bot = new Telegraf(botToken)

// Set webhook
bot.telegram.setWebhook('https://yourdomain.com/bot')

// Express.js example
import express from 'express'
const app = express()

app.use(bot.webhookCallback('/bot'))
app.listen(3000)
```

## Support

For issues or questions:
1. Check the main [README.md](./README.md)
2. Review [ARCHITECTURE.md](../../ARCHITECTURE.md) in CLI package
3. Open an issue on GitHub

## License

ISC

