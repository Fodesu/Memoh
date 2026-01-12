# Telegram Bot 快速开始

## 5 分钟启动指南

### 1. 获取 Bot Token

1. 在 Telegram 搜索 `@BotFather`
2. 发送 `/newbot`
3. 按提示输入 bot 名称和用户名
4. 复制获得的 token（格式类似：`123456789:ABCdefGHIjklMNOpqrsTUVwxyz`）

### 2. 准备环境

```bash
# 安装依赖
cd packages/platform-telegram
pnpm install

# 创建配置文件
cat > .env << EOF
BOT_TOKEN=你的_bot_token
REDIS_URL=redis://localhost:6379
API_BASE_URL=http://localhost:7002
EOF
```

### 3. 启动服务

**Terminal 1 - 启动 Redis:**
```bash
redis-server
```

**Terminal 2 - 启动 Memoh API:**
```bash
cd packages/api
pnpm start
```

**Terminal 3 - 启动 Telegram Bot:**
```bash
cd packages/platform-telegram
pnpm start
```

### 4. 测试 Bot

在 Telegram 中找到你的 bot，然后：

```
你: /start

Bot: 👋 Welcome to Memoh Bot!

Available commands:
/login <username> <password> - Login to your account
/logout - Logout from your account
/whoami - Show current user info
/chat <message> - Chat with AI agent
/help - Show this help message
```

```
你: /login admin password

Bot: ✅ Login successful!

👤 Username: admin
🎭 Role: admin
🔑 User ID: xxx

You can now use the bot to interact with Memoh.
```

```
你: /chat 你好，介绍一下你自己

Bot: 🤖 你好！我是 Memoh AI 助手...
```

```
你: 今天天气怎么样？

Bot: 🤖 很抱歉，我没有实时天气信息...
```

## 多用户测试

用两个不同的 Telegram 账号测试：

**账号 A:**
```
A: /login userA passwordA
Bot: ✅ Login successful! Username: userA

A: 我是用户A
Bot: 🤖 你好 userA...
```

**账号 B:**
```
B: /login userB passwordB
Bot: ✅ Login successful! Username: userB

B: 我是用户B
Bot: 🤖 你好 userB...
```

两个用户的对话是完全独立的！

## 常见问题

### Q: Bot 没有响应？

**A:** 检查以下几点：
1. Bot token 是否正确
2. Bot 是否在运行：`pnpm start`
3. 查看控制台是否有错误
4. 在 BotFather 中确认 bot 已创建

### Q: 登录失败？

**A:** 
1. 确认 API 是否在运行：`curl http://localhost:7002`
2. 检查用户名密码是否正确
3. 查看 API 日志

### Q: Redis 连接错误？

**A:**
1. 确认 Redis 在运行：`redis-cli ping` 应返回 `PONG`
2. 检查 `.env` 中的 `REDIS_URL`
3. 如果用 Docker：`docker run -d -p 6379:6379 redis`

### Q: 如何查看存储的数据？

**A:**
```bash
# 连接 Redis
redis-cli

# 查看所有 token
KEYS memoh:tg:token:*

# 查看特定用户的 token
GET memoh:tg:token:123456789

# 查看用户信息
GET memoh:tg:user:123456789

# 查看过期时间
TTL memoh:tg:token:123456789
```

## 下一步

- 📖 阅读 [README.md](./README.md) 了解更多功能
- 🛠 查看 [SETUP.md](./SETUP.md) 了解详细配置
- 🎯 参考 [example.ts](./example.ts) 学习编程使用
- 📚 阅读项目根目录的 [REFACTORING_SUMMARY.md](../../REFACTORING_SUMMARY.md) 了解整体架构

## 添加自定义命令

编辑 `src/index.ts`：

```typescript
// 添加一个新命令
this.bot.command('hello', requireAuth(storage), async (ctx) => {
  await ctx.reply('Hello! 你好！')
})
```

重启 bot，然后在 Telegram 中：

```
你: /hello

Bot: Hello! 你好！
```

## 生产部署提示

1. **使用环境变量** 而不是 `.env` 文件
2. **使用 HTTPS** 作为 API endpoint
3. **使用 PM2** 或 Docker 保持进程运行
4. **添加日志** 监控和调试
5. **设置 Webhook** 而不是 polling（更高效）

示例 PM2 配置：

```json
{
  "apps": [{
    "name": "memoh-tg-bot",
    "script": "bun",
    "args": "run src/bot.ts",
    "env": {
      "BOT_TOKEN": "your_token",
      "REDIS_URL": "redis://localhost:6379",
      "API_BASE_URL": "https://api.yourdomain.com"
    }
  }]
}
```

启动：
```bash
pm2 start ecosystem.json
pm2 logs memoh-tg-bot
```

祝你使用愉快！🎉

