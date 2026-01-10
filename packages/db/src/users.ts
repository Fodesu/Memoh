import { pgTable, pgEnum, text, timestamp, uuid, boolean } from 'drizzle-orm/pg-core'

// 定义用户角色枚举
export const userRoleEnum = pgEnum('user_role', ['admin', 'member'])

// 用户表
export const users = pgTable('users', {
  // 主键ID
  id: uuid('id').primaryKey().defaultRandom(),
  
  // 用户名（唯一）
  username: text('username').notNull().unique(),
  
  // 邮箱（可选，唯一）
  email: text('email').unique(),
  
  // 密码哈希值（使用 bcrypt 或其他加密方式）
  passwordHash: text('password_hash').notNull(),
  
  // 用户角色
  role: userRoleEnum('role').notNull().default('member'),
  
  // 显示名称
  displayName: text('display_name'),
  
  // 头像 URL
  avatarUrl: text('avatar_url'),
  
  // 账户状态（是否激活）
  isActive: boolean('is_active').notNull().default(true),
  
  // 创建时间
  createdAt: timestamp('created_at').notNull().defaultNow(),
  
  // 更新时间
  updatedAt: timestamp('updated_at').notNull().defaultNow(),
  
  // 最后登录时间
  lastLoginAt: timestamp('last_login_at'),
})
