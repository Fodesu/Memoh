# 用户表设计

## 数据库表结构

### users 表（用户表）

存储用户的基本信息和登录凭证。

| 字段名 | 类型 | 说明 | 约束 |
|--------|------|------|------|
| id | uuid | 用户唯一标识 | 主键，自动生成 |
| username | text | 用户名 | 非空，唯一 |
| email | text | 邮箱地址 | 唯一（可选）|
| password_hash | text | 密码哈希值 | 非空 |
| role | user_role | 用户角色 | 枚举：admin/member，默认 member |
| display_name | text | 显示名称 | 可选 |
| avatar_url | text | 头像地址 | 可选 |
| is_active | text | 账户状态 | 默认 true |
| created_at | timestamp | 创建时间 | 自动生成 |
| updated_at | timestamp | 更新时间 | 自动生成 |
| last_login_at | timestamp | 最后登录时间 | 可选 |

## 角色说明

系统定义了两种角色：
- **admin**: 管理员，拥有完整权限
- **member**: 普通成员，拥有基础权限

## 使用方法

### 1. 生成迁移文件

```bash
cd packages/db
pnpm run generate
```

### 2. 执行数据库迁移

```bash
pnpm run push
```

### 3. 在代码中使用

```typescript
import { users, userRoleEnum } from '@memoh/db/schema'
import { db } from '@memoh/db'

// 创建用户
const password = 'password123'
const passwordHash = await Bun.password.hash(password) // 使用 Bun 内置的密码加密

const newUser = await db.insert(users).values({
  username: 'john_doe',
  email: 'john@example.com',
  passwordHash: passwordHash,
  role: 'member',
  displayName: 'John Doe',
}).returning()

// 或者使用辅助函数
import { createUser } from '@memoh/db/src/user-helpers'

const user = await createUser({
  username: 'john_doe',
  email: 'john@example.com',
  passwordHash: await Bun.password.hash('password123'),
  role: 'member',
  displayName: 'John Doe',
})
```

## Root 用户

系统支持通过环境变量配置超级用户（Root User）：

```bash
ROOT_USER=admin
ROOT_USER_PASSWORD=your_secure_password
```

Root 用户特点：
- 不存储在数据库中
- 通过环境变量直接验证
- 始终拥有 admin 角色
- 用于系统初始化和紧急管理

## 安全建议

1. **密码存储**: 
   - 永远不要存储明文密码
   - 使用 Bun.password.hash 进行密码加密
   - Bun 内置的密码加密基于 bcrypt 算法，安全可靠

2. **身份验证**:
   - 使用 JWT 进行用户身份验证
   - 设置合理的过期时间（默认 7 天）
   - 在请求头中携带 `Authorization: Bearer <token>`

3. **权限控制**:
   - 在业务层实现基于角色的访问控制（RBAC）
   - 验证用户权限后再执行敏感操作
   - 记录重要操作的审计日志

4. **环境变量**:
   - 设置强密码作为 ROOT_USER_PASSWORD
   - 在生产环境中设置自定义的 JWT_SECRET
   - 不要将敏感信息提交到代码仓库

## 扩展建议

如果需要更复杂的功能，可以考虑：

1. 添加用户注册功能
2. 添加密码重置功能
3. 添加邮箱验证功能
4. 实现刷新令牌机制
5. 添加用户会话管理

