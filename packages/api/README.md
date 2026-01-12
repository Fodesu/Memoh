# @memoh/api

API 服务器，基于 Elysia 构建。

## API 模块

### 认证模块 (`/auth`)

用户认证和授权管理。

- `POST /auth/login` - 用户登录
- `GET /auth/verify` - 验证 token
- `GET /auth/me` - 获取当前用户信息

详细文档：[AUTH_README.md](./AUTH_README.md)

### 用户管理模块 (`/user`) 🔒 仅管理员

完整的用户 CRUD 操作接口。

- `GET /user` - 获取所有用户
- `GET /user/:id` - 获取单个用户
- `POST /user` - 创建用户
- `PUT /user/:id` - 更新用户信息
- `DELETE /user/:id` - 删除用户
- `PATCH /user/:id/password` - 更新用户密码

详细文档：[USER_MANAGEMENT.md](./USER_MANAGEMENT.md)

### 模型管理模块 (`/model`)

AI 模型配置管理。

- `GET /model` - 获取所有模型
- `GET /model/:id` - 获取单个模型
- `POST /model` - 创建模型配置
- `PUT /model/:id` - 更新模型配置
- `DELETE /model/:id` - 删除模型配置
- `GET /model/chat/default` - 获取默认聊天模型
- `GET /model/summary/default` - 获取默认摘要模型
- `GET /model/embedding/default` - 获取默认嵌入模型

### 设置模块 (`/settings`) 🔒 需要认证

用户偏好设置管理（自动使用当前登录用户）。

- `GET /settings` - 获取当前用户设置
- `PUT /settings` - 更新当前用户设置（支持模型配置、Agent 参数等）

详细文档：[SETTINGS_API.md](./SETTINGS_API.md)

### 记忆模块 (`/memory`) 🔒 需要认证

用户记忆和对话历史管理（自动使用当前登录用户）。

- `POST /memory` - 添加记忆
- `GET /memory/search` - 搜索记忆
- `GET /memory/message` - 获取消息历史（分页）
- `GET /memory/message/filter` - 按日期范围过滤消息

详细文档：[API_CHANGES.md](./API_CHANGES.md)

### Agent 模块 (`/agent`) 🔒 需要认证

AI Agent 智能对话接口，支持流式响应和记忆管理。

- `POST /agent/stream` - 流式对话（Server-Sent Events）

详细文档：[AGENT_API.md](./AGENT_API.md)

## 快速开始

### 安装依赖

```bash
pnpm install
```

### 配置环境变量

复制并编辑环境变量文件：

```bash
cp ../../.env.example ../../.env
```

必需配置：
- `DATABASE_URL` - PostgreSQL 连接字符串
- `ROOT_USER` - Root 超级管理员用户名
- `ROOT_USER_PASSWORD` - Root 超级管理员密码
- `JWT_SECRET` - JWT 签名密钥

### 启动开发服务器

```bash
pnpm run dev
```

服务器将在 `http://localhost:7002` 启动。

### 构建生产版本

```bash
pnpm run build
pnpm run start
```

## 认证和权限

### Bearer Token 认证

所有受保护的 API 端点需要在请求头中携带 JWT token：

```
Authorization: Bearer <your_jwt_token>
```

### 权限级别

- **公开接口**：无需认证
- **用户接口**：需要有效的 JWT token
- **管理员接口** 🔒：需要管理员角色的 JWT token

## 中间件

### `authMiddleware`

强制认证中间件，要求请求必须包含有效的 Bearer token。

```typescript
import { authMiddleware } from './middlewares'

const protectedModule = new Elysia()
  .use(authMiddleware)
  .get('/protected', ({ user }) => {
    return { message: `Hello ${user.username}!` }
  })
```

### `adminMiddleware` 🔒

管理员权限中间件，要求用户角色为 `admin`。

```typescript
import { adminMiddleware } from './middlewares'

const adminModule = new Elysia()
  .use(adminMiddleware)
  .get('/admin-only', ({ user }) => {
    return { message: 'Admin access granted' }
  })
```

### `optionalAuthMiddleware`

可选认证中间件，如果有 token 则验证，没有则 `user` 为 `null`。

```typescript
import { optionalAuthMiddleware } from './middlewares'

const publicModule = new Elysia()
  .use(optionalAuthMiddleware)
  .get('/public', ({ user }) => {
    if (user) {
      return { message: `Welcome back, ${user.username}!` }
    }
    return { message: 'Welcome, guest!' }
  })
```

## 运行测试

```bash
pnpm test
```

## 技术栈

- **Elysia** - 高性能 Web 框架
- **@elysiajs/jwt** - JWT 认证插件
- **@elysiajs/bearer** - Bearer token 提取插件
- **@elysiajs/cors** - CORS 支持
- **Drizzle ORM** - 数据库 ORM
- **Zod** - 数据验证
- **Bun** - JavaScript 运行时

## 相关文档

- [认证系统](./AUTH_README.md)
- [用户管理](./USER_MANAGEMENT.md)
- [项目设置指南](../../SETUP.md)
- [数据库 Schema](../db/USERS_SCHEMA.md)