import { db } from '@memohome/db'
import { users, settings } from '@memohome/db/schema'
import { eq } from 'drizzle-orm'

/**
 * 验证用户凭据
 * 优先检查是否为 ROOT 用户，否则查询数据库
 */
export const validateUser = async (username: string, password: string) => {
  // 检查是否为 ROOT 用户
  const rootUser = process.env.ROOT_USER
  const rootPassword = process.env.ROOT_USER_PASSWORD

  if (rootUser && rootPassword && username === rootUser) {
    if (password === rootPassword) {
      // 检查 root 用户的 settings 是否存在，不存在则创建
      const [existingSettings] = await db
        .select()
        .from(settings)
        .where(eq(settings.userId, 'root'))

      if (!existingSettings) {
        // 为 root 用户创建默认 settings
        await db
          .insert(settings)
          .values({
            userId: 'root',
            defaultChatModel: null,
            defaultEmbeddingModel: null,
            defaultSummaryModel: null,
            maxContextLoadTime: 60,
            language: 'Same as user input',
          })
          .onConflictDoNothing() // 避免并发创建导致的冲突
      }

      // 返回 ROOT 用户信息
      return {
        id: 'root',
        username: rootUser,
        role: 'admin' as const,
        displayName: 'Root User',
      }
    }
    return null
  }

  // 查询数据库中的用户
  const [user] = await db
    .select()
    .from(users)
    .where(eq(users.username, username))

  if (!user) {
    return null
  }

  // 验证密码 (这里使用简单的 Bun.password.verify)
  const isValid = await Bun.password.verify(password, user.passwordHash)
  
  if (!isValid) {
    return null
  }

  // 检查账户是否激活
  if (!user.isActive) {
    return null
  }

  // 更新最后登录时间
  await db
    .update(users)
    .set({
      lastLoginAt: new Date(),
    })
    .where(eq(users.id, user.id))

  return {
    id: user.id,
    username: user.username,
    role: user.role,
    displayName: user.displayName || user.username,
    email: user.email,
  }
}

