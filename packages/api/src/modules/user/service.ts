import { db } from '@memohome/db'
import { users, settings } from '@memohome/db/schema'
import { eq, sql, desc, asc } from 'drizzle-orm'
import type { CreateUserInput, UpdateUserInput } from './model'
import { calculateOffset, createPaginatedResult, type PaginatedResult } from '../../utils/pagination'

/**
 * 用户列表返回类型
 */
type UserListItem = {
  id: string
  username: string
  email: string | null
  role: 'admin' | 'member'
  displayName: string | null
  avatarUrl: string | null
  isActive: boolean
  createdAt: Date
  updatedAt: Date
  lastLoginAt: Date | null
}

/**
 * 获取所有用户列表（支持分页）
 */
export const getUsers = async (params?: {
  page?: number
  limit?: number
  sortBy?: string
  sortOrder?: 'asc' | 'desc'
}): Promise<PaginatedResult<UserListItem>> => {
  const page = params?.page || 1
  const limit = params?.limit || 10
  const sortBy = params?.sortBy || 'createdAt'
  const sortOrder = params?.sortOrder || 'desc'
  const offset = calculateOffset(page, limit)

  // 获取总数
  const [{ count }] = await db
    .select({ count: sql<number>`count(*)` })
    .from(users)

  // 动态排序
  const orderColumn = sortBy === 'username' ? users.username :
                      sortBy === 'email' ? users.email :
                      sortBy === 'role' ? users.role :
                      sortBy === 'updatedAt' ? users.updatedAt :
                      users.createdAt

  const orderFn = sortOrder === 'desc' ? desc : asc

  // 获取分页数据
  const userList = await db
    .select({
      id: users.id,
      username: users.username,
      email: users.email,
      role: users.role,
      displayName: users.displayName,
      avatarUrl: users.avatarUrl,
      isActive: users.isActive,
      createdAt: users.createdAt,
      updatedAt: users.updatedAt,
      lastLoginAt: users.lastLoginAt,
    })
    .from(users)
    .orderBy(orderFn(orderColumn))
    .limit(limit)
    .offset(offset)

  return createPaginatedResult(userList, Number(count), page, limit)
}

/**
 * 根据 ID 获取用户
 */
export const getUserById = async (id: string) => {
  const [user] = await db
    .select({
      id: users.id,
      username: users.username,
      email: users.email,
      role: users.role,
      displayName: users.displayName,
      avatarUrl: users.avatarUrl,
      isActive: users.isActive,
      createdAt: users.createdAt,
      updatedAt: users.updatedAt,
      lastLoginAt: users.lastLoginAt,
    })
    .from(users)
    .where(eq(users.id, id))

  return user
}

/**
 * 创建新用户
 */
export const createUser = async (data: CreateUserInput) => {
  // 检查用户名是否已存在
  const [existingUser] = await db
    .select()
    .from(users)
    .where(eq(users.username, data.username))

  if (existingUser) {
    throw new Error('Username already exists')
  }

  // 检查邮箱是否已存在（如果提供了邮箱）
  if (data.email) {
    const [existingEmail] = await db
      .select()
      .from(users)
      .where(eq(users.email, data.email))

    if (existingEmail) {
      throw new Error('Email already exists')
    }
  }

  // 加密密码
  const passwordHash = await Bun.password.hash(data.password)

  // 创建用户
  const [newUser] = await db
    .insert(users)
    .values({
      username: data.username,
      email: data.email || null,
      passwordHash,
      role: data.role || 'member',
      displayName: data.displayName || null,
      avatarUrl: data.avatarUrl || null,
    })
    .returning({
      id: users.id,
      username: users.username,
      email: users.email,
      role: users.role,
      displayName: users.displayName,
      avatarUrl: users.avatarUrl,
      isActive: users.isActive,
      createdAt: users.createdAt,
    })

  // 自动创建用户的 settings 条目（使用默认值）
  await db
    .insert(settings)
    .values({
      userId: newUser.id,
      defaultChatModel: null,
      defaultEmbeddingModel: null,
      defaultSummaryModel: null,
      maxContextLoadTime: 60,
      language: 'Same as user input',
    })

  return newUser
}

/**
 * 更新用户信息
 */
export const updateUser = async (id: string, data: UpdateUserInput) => {
  // 检查用户是否存在
  const existingUser = await getUserById(id)
  if (!existingUser) {
    return null
  }

  // 如果更新邮箱，检查邮箱是否已被其他用户使用
  if (data.email) {
    const [emailUser] = await db
      .select()
      .from(users)
      .where(eq(users.email, data.email))

    if (emailUser && emailUser.id !== id) {
      throw new Error('Email already exists')
    }
  }

  // 更新用户
  const [updatedUser] = await db
    .update(users)
    .set({
      ...data,
      updatedAt: new Date(),
    })
    .where(eq(users.id, id))
    .returning({
      id: users.id,
      username: users.username,
      email: users.email,
      role: users.role,
      displayName: users.displayName,
      avatarUrl: users.avatarUrl,
      isActive: users.isActive,
      createdAt: users.createdAt,
      updatedAt: users.updatedAt,
      lastLoginAt: users.lastLoginAt,
    })

  return updatedUser
}

/**
 * 删除用户
 */
export const deleteUser = async (id: string) => {
  // 检查用户是否存在
  const existingUser = await getUserById(id)
  if (!existingUser) {
    return null
  }

  const [deletedUser] = await db
    .delete(users)
    .where(eq(users.id, id))
    .returning({
      id: users.id,
      username: users.username,
    })

  return deletedUser
}

/**
 * 更新用户密码
 */
export const updateUserPassword = async (id: string, password: string) => {
  // 检查用户是否存在
  const existingUser = await getUserById(id)
  if (!existingUser) {
    return null
  }

  // 加密新密码
  const passwordHash = await Bun.password.hash(password)

  // 更新密码
  const [updatedUser] = await db
    .update(users)
    .set({
      passwordHash,
      updatedAt: new Date(),
    })
    .where(eq(users.id, id))
    .returning({
      id: users.id,
      username: users.username,
    })

  return updatedUser
}

