# Memoh CLI 架构说明

## 项目重构概述

本项目已重构为两个清晰分离的层次：

1. **Core 层** (`src/core/`): 纯粹的功能函数，不依赖任何 CLI UI 库
2. **CLI 层** (`src/cli/`): 命令行交互界面，负责用户输入输出

## 目录结构

```
src/
├── core/              # 核心功能层（可被其他项目使用）
│   ├── index.ts       # 统一导出所有核心功能
│   ├── auth.ts        # 认证相关功能
│   ├── user.ts        # 用户管理功能
│   ├── model.ts       # 模型配置功能
│   ├── agent.ts       # AI 对话功能
│   ├── memory.ts      # 记忆管理功能
│   ├── schedule.ts    # 定时任务功能
│   ├── settings.ts    # 设置管理功能
│   ├── debug.ts       # 调试工具
│   ├── config.ts      # 配置管理
│   └── client.ts      # API 客户端
├── cli/               # CLI 交互层
│   ├── index.ts       # CLI 入口
│   └── commands/      # 命令定义
│       ├── auth.ts
│       ├── user.ts
│       ├── model.ts
│       ├── agent.ts
│       ├── memory.ts
│       ├── schedule.ts
│       ├── config.ts
│       └── debug.ts
├── types/             # 类型定义
│   └── index.ts
├── utils/             # 工具函数
│   └── index.ts
└── index.ts           # 主导出文件
```

## Core 层特点

Core 层提供纯粹的功能函数，特点：

- ✅ **无 UI 依赖**: 不使用 chalk, ora, inquirer 等 CLI UI 库
- ✅ **类型安全**: 提供完整的 TypeScript 类型定义
- ✅ **错误处理**: 通过 throw Error 返回错误，调用者可自行处理
- ✅ **可复用**: 可被 CLI 或其他项目（如 Web 应用）导入使用
- ✅ **单一职责**: 每个模块只负责特定功能域

### Core 层 API 示例

```typescript
// Auth
import { login, logout, getCurrentUser } from '@memoh/cli/core'

await login({ username: 'admin', password: 'password' })
const user = await getCurrentUser()
logout()

// Agent
import { chat, chatStream } from '@memoh/cli/core'

// 非流式对话
const response = await chat({ 
  message: 'Hello',
  language: 'Chinese'
})

// 流式对话
await chatStream({ message: 'Hello' }, async (event) => {
  if (event.type === 'text-delta') {
    console.log(event.text)
  }
})

// Model
import { listModels, createModel } from '@memoh/cli/core'

const models = await listModels()
const newModel = await createModel({
  name: 'GPT-4',
  modelId: 'gpt-4',
  baseUrl: 'https://api.openai.com/v1',
  apiKey: 'sk-xxx',
  clientType: 'openai',
  type: 'chat'
})

// Memory
import { searchMemory, addMemory, getMessages } from '@memoh/cli/core'

const memories = await searchMemory({ query: 'test', limit: 10 })
await addMemory({ content: 'Important note' })
const messages = await getMessages({ page: 1, limit: 20 })
```

## CLI 层特点

CLI 层负责用户交互，特点：

- ✅ **命令定义**: 使用 commander.js 定义命令
- ✅ **美化输出**: 使用 chalk 颜色、ora 加载动画
- ✅ **交互输入**: 使用 inquirer 提示用户输入
- ✅ **错误显示**: 友好的错误信息展示
- ✅ **调用 Core**: 所有业务逻辑调用 Core 层函数

## 使用方式

### 1. 作为 CLI 使用

```bash
# 安装
pnpm install

# 运行命令
memoh auth login
memoh agent chat "Hello"
memoh model list
```

### 2. 作为库使用

在其他项目中导入：

```typescript
// 导入所有功能
import * as memoh from '@memoh/cli'

// 或导入特定模块
import { login, chat, listModels } from '@memoh/cli'
import * as auth from '@memoh/cli/core'
import type { User, Model } from '@memoh/cli/types'

// 使用
await memoh.login({ username: 'admin', password: 'password' })
const response = await memoh.chat({ message: 'Hello' })
const models = await memoh.listModels()
```

## 包导出

`package.json` 中的导出配置：

```json
{
  "exports": {
    ".": "./src/index.ts",           // 主入口，导出所有 core 功能
    "./core": "./src/core/index.ts", // Core 层
    "./types": "./src/types/index.ts", // 类型定义
    "./utils": "./src/utils/index.ts"  // 工具函数
  },
  "bin": {
    "memoh": "./src/cli/index.ts"  // CLI 入口
  }
}
```

## 设计原则

1. **关注点分离**: CLI UI 和业务逻辑完全分离
2. **可测试性**: Core 层可以独立测试，无需模拟 CLI 环境
3. **可复用性**: Core 层可在不同环境使用（CLI、Web、Desktop 等）
4. **类型安全**: 完整的 TypeScript 类型定义
5. **错误处理**: 统一的错误处理机制

## 迁移指南

如果你之前使用旧版本的代码，迁移方式：

### 旧代码（CLI 层调用）
```typescript
// 这些只能在 CLI 中使用，有 UI 输出
```

### 新代码（Core 层调用）
```typescript
// 可以在任何地方使用，无 UI 依赖
import { login, chat } from '@memoh/cli'

try {
  await login({ username: 'admin', password: 'password' })
  const response = await chat({ message: 'Hello' })
  console.log(response)
} catch (error) {
  console.error('Error:', error.message)
}
```

## 开发指南

### 添加新功能

1. 在 `src/core/` 中添加新的核心功能模块
2. 在 `src/core/index.ts` 中导出
3. 在 `src/cli/commands/` 中添加对应的 CLI 命令
4. 在 `src/cli/index.ts` 中注册命令

### 测试

Core 层可以直接测试：

```typescript
import { login, chat } from '../src/core'

test('login should work', async () => {
  const result = await login({ username: 'test', password: 'test' })
  expect(result.success).toBe(true)
})
```

## 注意事项

- Core 层函数通过 `throw Error` 返回错误，调用者需要处理
- CLI 层负责美化错误信息和用户反馈
- 配置文件位于 `~/.memoh/config.json`
- API 客户端使用 Eden Treaty

