import { db } from '@memoh/db'
import { platform } from '@memoh/db/schema'
import { Platform } from '@memoh/shared'
import { eq, sql, desc, asc } from 'drizzle-orm'
import { calculateOffset, createPaginatedResult, type PaginatedResult } from '../../utils/pagination'
import path from 'node:path'

/**
 * 平台列表返回类型
 */
type PlatformListItem = {
  id: string
  name: string
  endpoint: string
  config: Record<string, unknown>
  active: boolean
  createdAt: Date
  updatedAt: Date
}

export const getPlatforms = async (params?: {
  page?: number
  limit?: number
  sortOrder?: 'asc' | 'desc'
}): Promise<PaginatedResult<PlatformListItem>> => {
  const page = params?.page || 1
  const limit = params?.limit || 10
  const sortOrder = params?.sortOrder || 'desc'
  const offset = calculateOffset(page, limit)

  // 获取总数
  const [{ count }] = await db
    .select({ count: sql<number>`count(*)` })
    .from(platform)

  // 获取分页数据
  const orderFn = sortOrder === 'desc' ? desc : asc
  const platforms = await db
    .select()
    .from(platform)
    .orderBy(orderFn(platform.createdAt))
    .limit(limit)
    .offset(offset)

  // Cast config to Record<string, unknown> for type safety
  const typedPlatforms = platforms.map(p => ({
    ...p,
    config: p.config as Record<string, unknown>,
  }))

  return createPaginatedResult(typedPlatforms, Number(count), page, limit)
}

export const getPlatformById = async (id: string) => {
  const [result] = await db.select().from(platform).where(eq(platform.id, id))
  return result
}

export const getPlatformByName = async (name: string) => {
  const [result] = await db.select().from(platform).where(eq(platform.name, name))
  return result
}

export const getActivePlatforms = async () => {
  return await db.select()
    .from(platform)
    .where(eq(platform.active, true))
}

export const createPlatform = async (data: Omit<Platform, 'id'>) => {
  const [newPlatform] = await db
    .insert(platform)
    .values({
      name: data.name,
      endpoint: data.endpoint,
      config: data.config,
      active: data.active ?? true,
    })
    .returning()
  if (data.active ?? true) {
    await activePlatform({
      id: newPlatform.id,
      name: newPlatform.name,
      endpoint: newPlatform.endpoint,
      config: newPlatform.config as Record<string, unknown>,
      active: newPlatform.active,
    })
  }
  return newPlatform
}

export const updatePlatform = async (id: string, data: Partial<Omit<Platform, 'id'>>) => {
  const updateData: {
    name?: string
    endpoint?: string
    config?: Record<string, unknown>
    active?: boolean
    updatedAt: Date
  } = {
    updatedAt: new Date(),
  }

  if (data.name !== undefined) updateData.name = data.name
  if (data.endpoint !== undefined) updateData.endpoint = data.endpoint
  if (data.config !== undefined) updateData.config = data.config
  if (data.active !== undefined) updateData.active = data.active

  const [updatedPlatform] = await db
    .update(platform)
    .set(updateData)
    .where(eq(platform.id, id))
    .returning()
  return updatedPlatform
}

export const deletePlatform = async (id: string) => {
  const [deletedPlatform] = await db
    .delete(platform)
    .where(eq(platform.id, id))
    .returning()
  return deletedPlatform
}

export const updatePlatformConfig = async (id: string, config: Record<string, unknown>) => {
  const [updatedPlatform] = await db
    .update(platform)
    .set({
      config,
      updatedAt: new Date(),
    })
    .where(eq(platform.id, id))
    .returning()
  return updatedPlatform
}

// active

export const activePlatform = async (platform: Platform) => {
  await fetch(path.join(platform.endpoint, '/start'), {
    method: 'POST',
    body: JSON.stringify(platform.config),
    headers: {
      'Content-Type': 'application/json',
    },
  })
}

export const inactivePlatform = async (platform: Platform) => {
  await fetch(path.join(platform.endpoint, '/stop'), {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
  })
}

export const setActivePlatform = async (id: string, active: boolean) => {
  const currentPlatform = await getPlatformById(id)
  if (!currentPlatform) {
    throw new Error('Platform not found')
  }
  const platformData: Platform = {
    id: currentPlatform.id,
    name: currentPlatform.name,
    endpoint: currentPlatform.endpoint,
    config: currentPlatform.config as Record<string, unknown>,
    active: active,
  }
  if (active) {
    await activePlatform(platformData)
  } else {
    await inactivePlatform(platformData)
  }
  const [updatedPlatform] = await db
    .update(platform)
    .set({ active })
    .where(eq(platform.id, id))
    .returning()
  return updatedPlatform
}

export const sendMessageToPlatform = async (name: string, options: {
  message: string
  userId: string
}) => {
  const currentPlatform = await getPlatformByName(name)
  if (!currentPlatform) {
    throw new Error('Platform not found')
  }
  await fetch(path.join(currentPlatform.endpoint, '/send'), {
    method: 'POST',
    body: JSON.stringify(options),
    headers: {
      'Content-Type': 'application/json',
    },
  })
}
