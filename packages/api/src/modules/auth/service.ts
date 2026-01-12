import { db } from '@memoh/db'
import { users, settings } from '@memoh/db/schema'
import { eq } from 'drizzle-orm'

/**
 * 验证用户凭据
 * 优先检查是否为 ROOT 用户，否则查询数据库
 */
export const validateUser = async (username: string, password: string) => {
  // 检查是否为 ROOT 用户
  const rootUser = process.env.ROOT_USER
  const rootPassword = process.env.ROOT_USER_PASSWORD

  let userId: string | null = null

  if (rootUser && rootPassword && username === rootUser) {
    if (password === rootPassword) {
      // 检查 root 用户是否存在于数据库中
      const [existingUser] = await db
        .select()
        .from(users)
        .where(eq(users.username, rootUser))

      userId = existingUser?.id
      if (!existingUser) {
        // 为 root 用户创建数据库记录
        // 使用占位符密码哈希，因为实际密码在环境变量中
        const [newUser] = await db
          .insert(users)
          .values({
            username: rootUser,
            passwordHash: 'ENV_BASED_AUTH', // 占位符，实际使用环境变量验证
            role: 'admin',
            displayName: 'Root User',
            email: null,
            avatarUrl: null,
            isActive: true,
          })
          .onConflictDoNothing() // 避免并发创建导致的冲突
          .returning({
            id: users.id,
          })

        userId = newUser.id
      }

      // 检查 root 用户的 settings 是否存在，不存在则创建
      const [existingSettings] = await db
        .select()
        .from(settings)
        .where(eq(settings.userId, userId))

      if (!existingSettings) {
        // 为 root 用户创建默认 settings
        await db
          .insert(settings)
          .values({
            userId: userId,
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
        id: userId,
        username: rootUser,
        role: 'admin' as const,
        displayName: 'Root User',
      }
    }
    return null
  }

  // 查询数据库中的用户（使用 username 而不是 id）
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

