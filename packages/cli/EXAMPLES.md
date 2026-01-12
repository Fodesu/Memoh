# Memoh CLI 使用示例

## 作为 CLI 使用

### 认证
```bash
# 登录
memoh auth login -u admin -p password

# 查看当前用户
memoh auth whoami

# 登出
memoh auth logout

# 配置 API URL
memoh auth config -s http://localhost:7002
```

### AI 对话
```bash
# 单次对话
memoh agent chat "今天天气怎么样？"

# 交互模式
memoh agent interactive

# 指定语言和上下文时间
memoh agent chat "Hello" -l English -t 30
```

### 模型管理
```bash
# 列出所有模型
memoh model list

# 创建模型配置
memoh model create \
  -n "GPT-4" \
  -m "gpt-4" \
  -u "https://api.openai.com/v1" \
  -k "sk-xxx" \
  -c openai \
  -t chat

# 查看默认模型
memoh model defaults

# 获取模型详情
memoh model get <model-id>

# 删除模型
memoh model delete <model-id>
```

### 用户管理
```bash
# 列出所有用户
memoh user list

# 创建用户
memoh user create -u newuser -p password -r user

# 查看用户
memoh user get <user-id>

# 更新密码
memoh user update-password <user-id> -p newpassword

# 删除用户
memoh user delete <user-id>
```

### 记忆管理
```bash
# 搜索记忆
memoh memory search "关键词" -l 10

# 添加记忆
memoh memory add "重要的事情"

# 查看消息历史
memoh memory messages -p 1 -l 20

# 按日期过滤消息
memoh memory filter -s 2024-01-01T00:00:00Z -e 2024-12-31T23:59:59Z
```

### 定时任务
```bash
# 列出所有任务
memoh schedule list

# 创建任务
memoh schedule create -t "每日报告" -c "0 9 * * *" -e

# 查看任务详情
memoh schedule get <schedule-id>

# 更新任务
memoh schedule update <schedule-id> -t "新标题" --enabled true

# 切换任务状态
memoh schedule toggle <schedule-id>

# 删除任务
memoh schedule delete <schedule-id>
```

### 配置管理
```bash
# 查看配置
memoh config get

# 设置配置
memoh config set --language Chinese --max-context-time 60 --chat-model <model-id>

# 交互式配置向导
memoh config setup
```

### 调试工具
```bash
# 测试 API 连接
memoh debug ping
```

## 作为库使用

### 基础使用

```typescript
import * as memoh from '@memoh/cli'

async function main() {
  try {
    // 登录
    const loginResult = await memoh.login({
      username: 'admin',
      password: 'password'
    })
    console.log('登录成功:', loginResult.user?.username)

    // 获取当前用户
    const user = await memoh.getCurrentUser()
    console.log('当前用户:', user.username)

    // AI 对话
    const response = await memoh.chat({
      message: 'Hello',
      language: 'Chinese'
    })
    console.log('AI 回复:', response)

    // 列出模型
    const models = await memoh.listModels()
    console.log('模型数量:', models.length)

    // 登出
    memoh.logout()
  } catch (error) {
    console.error('错误:', error.message)
  }
}

main()
```

### 流式对话

```typescript
import { chatStream } from '@memoh/cli'

async function streamChat() {
  await chatStream(
    {
      message: '讲一个故事',
      language: 'Chinese'
    },
    async (event) => {
      if (event.type === 'text-delta') {
        process.stdout.write(event.text || '')
      } else if (event.type === 'tool-call') {
        console.log(`\n[使用工具: ${event.toolName}]`)
      } else if (event.type === 'error') {
        console.error('\n错误:', event.error)
      } else if (event.type === 'done') {
        console.log('\n完成')
      }
    }
  )
}

streamChat()
```

### 模型管理

```typescript
import { createModel, listModels, getDefaultModels } from '@memoh/cli'

async function manageModels() {
  // 创建 Chat 模型
  const chatModel = await createModel({
    name: 'GPT-4',
    modelId: 'gpt-4',
    baseUrl: 'https://api.openai.com/v1',
    apiKey: 'sk-xxx',
    clientType: 'openai',
    type: 'chat'
  })
  console.log('创建的模型:', chatModel.id)

  // 创建 Embedding 模型
  const embeddingModel = await createModel({
    name: 'Text Embedding',
    modelId: 'text-embedding-3-small',
    baseUrl: 'https://api.openai.com/v1',
    apiKey: 'sk-xxx',
    clientType: 'openai',
    type: 'embedding',
    dimensions: 1536
  })
  console.log('Embedding 模型:', embeddingModel.id)

  // 列出所有模型
  const models = await listModels()
  models.forEach(item => {
    console.log(`- ${item.model.name} (${item.model.type})`)
  })

  // 获取默认模型
  const defaults = await getDefaultModels()
  console.log('默认 Chat 模型:', defaults.chat?.name)
  console.log('默认 Embedding 模型:', defaults.embedding?.name)
}

manageModels()
```

### 记忆管理

```typescript
import { searchMemory, addMemory, getMessages } from '@memoh/cli'

async function manageMemory() {
  // 添加记忆
  await addMemory({ content: '今天学习了 TypeScript' })

  // 搜索记忆
  const memories = await searchMemory({
    query: 'TypeScript',
    limit: 5
  })
  
  memories.forEach(memory => {
    console.log(`相似度: ${(memory.similarity! * 100).toFixed(2)}%`)
    console.log(`内容: ${memory.content}`)
    console.log('---')
  })

  // 获取消息历史
  const result = await getMessages({ page: 1, limit: 20 })
  console.log(`总消息数: ${result.pagination.total}`)
  result.messages.forEach(msg => {
    console.log(`${msg.role}: ${msg.content}`)
  })
}

manageMemory()
```

### 用户管理

```typescript
import { createUser, listUsers, updateUserPassword } from '@memoh/cli'

async function manageUsers() {
  // 创建用户
  const newUser = await createUser({
    username: 'testuser',
    password: 'password123',
    role: 'user'
  })
  console.log('创建的用户:', newUser.username)

  // 列出所有用户
  const users = await listUsers()
  console.log('用户数量:', users.length)

  // 更新密码
  await updateUserPassword({
    userId: newUser.id,
    password: 'newpassword123'
  })
  console.log('密码已更新')
}

manageUsers()
```

### 定时任务管理

```typescript
import { createSchedule, listSchedules, toggleSchedule } from '@memoh/cli'

async function manageSchedules() {
  // 创建定时任务
  const schedule = await createSchedule({
    title: '每日报告',
    description: '生成每日工作报告',
    cronExpression: '0 9 * * *',
    enabled: true
  })
  console.log('创建的任务:', schedule.title)

  // 列出所有任务
  const schedules = await listSchedules()
  schedules.forEach(s => {
    console.log(`${s.title}: ${s.cronExpression} (${s.enabled ? '启用' : '禁用'})`)
  })

  // 切换任务状态
  const newStatus = await toggleSchedule(schedule.id)
  console.log('任务状态:', newStatus ? '启用' : '禁用')
}

manageSchedules()
```

### 配置管理

```typescript
import { getSettings, updateSettings, setConfig } from '@memoh/cli'

async function manageConfig() {
  // 设置 API URL
  setConfig('http://localhost:7002')

  // 获取用户设置
  const settings = await getSettings()
  console.log('当前语言:', settings.language)
  console.log('上下文时间:', settings.maxContextLoadTime)

  // 更新设置
  await updateSettings({
    language: 'English',
    maxContextLoadTime: 120,
    defaultChatModel: 'model-id-xxx'
  })
  console.log('设置已更新')
}

manageConfig()
```

### 错误处理

```typescript
import { login, chat } from '@memoh/cli'

async function handleErrors() {
  try {
    await login({
      username: 'wrong',
      password: 'wrong'
    })
  } catch (error) {
    if (error instanceof Error) {
      console.error('登录失败:', error.message)
    }
  }

  try {
    // 未登录时调用需要认证的 API
    await chat({ message: 'Hello' })
  } catch (error) {
    if (error instanceof Error) {
      console.error('需要先登录:', error.message)
    }
  }
}

handleErrors()
```

### 类型安全

```typescript
import type { User, Model, Memory, Schedule } from '@memoh/cli/types'
import { listUsers, listModels } from '@memoh/cli'

async function withTypes() {
  // 类型自动推导
  const users: User[] = await listUsers()
  const models = await listModels()
  
  // 使用类型
  users.forEach((user: User) => {
    console.log(`${user.username} (${user.role})`)
  })
  
  models.forEach((item) => {
    const model: Model = item.model
    console.log(`${model.name}: ${model.type}`)
  })
}

withTypes()
```

## Web 应用集成示例

```typescript
// api/memoh.ts
import * as memoh from '@memoh/cli'

export class MemohService {
  async login(username: string, password: string) {
    try {
      return await memoh.login({ username, password })
    } catch (error) {
      throw new Error('登录失败')
    }
  }

  async chat(message: string, onChunk: (text: string) => void) {
    await memoh.chatStream(
      { message },
      async (event) => {
        if (event.type === 'text-delta' && event.text) {
          onChunk(event.text)
        }
      }
    )
  }

  async getModels() {
    return await memoh.listModels()
  }
}

// 在 React 组件中使用
import { useState } from 'react'
import { MemohService } from './api/memoh'

function ChatComponent() {
  const [message, setMessage] = useState('')
  const [response, setResponse] = useState('')
  const service = new MemohService()

  const handleChat = async () => {
    setResponse('')
    await service.chat(message, (chunk) => {
      setResponse(prev => prev + chunk)
    })
  }

  return (
    <div>
      <input value={message} onChange={e => setMessage(e.target.value)} />
      <button onClick={handleChat}>发送</button>
      <div>{response}</div>
    </div>
  )
}
```

## 测试示例

```typescript
import { describe, test, expect, beforeAll } from 'bun:test'
import { login, logout, chat, createModel } from '@memoh/cli'

describe('Memoh Core API', () => {
  beforeAll(async () => {
    // 测试前登录
    await login({ username: 'test', password: 'test' })
  })

  test('chat should return response', async () => {
    const response = await chat({ message: 'Hello' })
    expect(response).toBeDefined()
    expect(typeof response).toBe('string')
  })

  test('createModel should work', async () => {
    const model = await createModel({
      name: 'Test Model',
      modelId: 'test-model',
      baseUrl: 'https://api.test.com',
      apiKey: 'test-key',
      clientType: 'openai',
      type: 'chat'
    })
    expect(model.id).toBeDefined()
    expect(model.name).toBe('Test Model')
  })

  test('should throw error when not logged in', async () => {
    logout()
    await expect(chat({ message: 'Hello' })).rejects.toThrow()
  })
})
```

