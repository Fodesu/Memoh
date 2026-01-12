<div align="center">
  <h1>Memoh</h1>
  <p>Long-memory, self-hosted, AI-powered personal housekeeper and lifemate.</p>
</div>

Memoh是一个专属于你的AI私人管家，你可以把它跑在你的NAS，路由器等个人设备上，24小时的为你提供服务。

## Features

- [x] 长记忆：Memoh拥有长记忆能力，可以为你的家庭成员提供个性化的服务。他会存储最近一段时间（默认最近15个小时）的上下文，超出时间后则会根据你的需求按需加载记忆
- [x] 定时任务：Memoh可以帮你创建智能的定时任务，比如：每天早上七点生成一个早餐菜谱，通过Telegram发送给我
- [x] 聊天软件支持：Memoh可以支持多种聊天软件，比如：Telegram，微信，QQ等常用社交软件，通过直接发送消息与Memoh进行交互，同时Memoh也可以通过事件触发，选择工具主动给你发送消息
- [ ] 文件系统管理：Memoh可以帮你管理你的文件系统，比如：文件搜索，图片分类，文件分享等。他可以创建文件，也可以通过聊天软件发送文件给你；你也可以通过发送文件给他帮你处理。
- [ ] MCP支持：Memoh可以支持多种MCP接口，与多种外部工具进行交互。
- More...

## Message Platforms
- [x] Telegram ([Telegram配置](#telegram-bot))
- [ ] Wechat
- [ ] Lark

## Quick Start

环境：
- PostgreSQL 16+
- Bun 1.2+
- PNPM
- Qdrant

```bash
cp .env.example .env
pnpm install
```

<details><summary>Environment Variables</summary>
- `DATABASE_URL`: PostgreSQL 连接字符串
- `ROOT_USER`: 超级管理员用户名
- `ROOT_USER_PASSWORD`: 超级管理员密码
- `JWT_SECRET`: JWT 签名密钥
- `QDRANT_URL`: Qdrant 连接字符串
- `REDIS_URL`: Redis 连接字符串
</details>

### 数据库初始化

```bash
pnpm run db:push
```

### API Server

```bash
pnpm run api:dev
```

API服务将在 `http://localhost:7002` 启动。

### 命令行工具

首先你需要登录：
```bash
pnpm cli auth login
```

按照提示输入管理员用户名和密码。


直接运行以下命令会进入Agent交互模式

```bash
pnpm cli
```

在此之前你需要配置模型，你至少需要配置一个聊天模型，一个嵌入模型，一个摘要模型。

```bash
pnpm run model:create --name "GPT-4" --model-id "gpt-4" --base-url "https://api.openai.com/v1" --api-key "your-api-key" --client-type "openai" --type "chat"
```
- `--name`: 模型显示名称
- `--model-id`: 模型ID
- `--base-url`: 模型API地址
- `--api-key`: 模型API密钥
- `--client-type`: 模型提供者类型, 可选值为 `openai` 或 `anthropic` 或 `google`
- `--type`: 模型类型，可选值为 `chat` 或 `embedding`

创建成功后你会得到一个uuid，你可以通过这个uuid来配置你的设置：

```bash
pnpm cli config set --chat-model <uuid> --summary-model <uuid> --embedding-model <uuid>
```
- `--chat-model`: 聊天模型uuid
- `--summary-model`: 摘要模型uuid
- `--embedding-model`: 嵌入模型uuid

然后你就可以正常的使用Memoh了。

你可以设置你的最大上下文加载时间，默认是900分钟，你可以通过以下命令来设置：

```bash
pnpm cli config set --max-context-time <minutes>
```
- `--max-context-time`: 最大上下文加载时间，单位为分钟

## Telegram Bot

你需要获取你的Telegram Bot Token， 然后启动Telegram Service：

```bash
pnpm telegram:start
```

Telegram Service将在 `http://localhost:7101` 启动，这个是endpoint，你需要在Memoh中配置你的Telegram Bot Token：

使用Memoh Cli:

```bash
pnpm cli platform create
```

根据提示配置platform
- name: telegram
- endpoint: http://localhost:7101
- config: { "botToken": "<your-telegram-bot-token>" }

然后你就可以通过Telegram Bot与Memoh进行交互了。
