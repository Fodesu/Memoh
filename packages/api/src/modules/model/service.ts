import { db } from '@memohome/db'
import { model } from '@memohome/db/schema'
import { Model } from '@memohome/shared'
import { eq, sql, desc, asc } from 'drizzle-orm'
import { getSettings } from '@/modules/settings/service'
import { calculateOffset, createPaginatedResult, type PaginatedResult } from '../../utils/pagination'

/**
 * 模型列表返回类型
 */
type ModelListItem = {
  id: string
  model: Model
}

export const getModels = async (params?: {
  page?: number
  limit?: number
  sortOrder?: 'asc' | 'desc'
}): Promise<PaginatedResult<ModelListItem>> => {
  const page = params?.page || 1
  const limit = params?.limit || 10
  const sortOrder = params?.sortOrder || 'desc'
  const offset = calculateOffset(page, limit)

  // 获取总数
  const [{ count }] = await db
    .select({ count: sql<number>`count(*)` })
    .from(model)

  // 获取分页数据（按 id 排序，因为 model 表没有 createdAt）
  const orderFn = sortOrder === 'desc' ? desc : asc
  const models = await db
    .select()
    .from(model)
    .orderBy(orderFn(model.id))
    .limit(limit)
    .offset(offset)

  return createPaginatedResult(models, Number(count), page, limit)
}

export const getModelById = async (id: string) => {
  const [result] = await db.select().from(model).where(eq(model.id, id))
  return result
}

export const createModel = async (data: Model) => {
  const [newModel] = await db
    .insert(model)
    .values({ model: data })
    .returning()
  return newModel
}

export const updateModel = async (id: string, data: Model) => {
  const [updatedModel] = await db
    .update(model)
    .set({ model: data })
    .where(eq(model.id, id))
    .returning()
  return updatedModel
}

export const deleteModel = async (id: string) => {
  const [deletedModel] = await db
    .delete(model)
    .where(eq(model.id, id))
    .returning()
  return deletedModel
}

export const getChatModel = async (userId: string) => {
  const userSettings = await getSettings(userId)
  if (!userSettings?.defaultChatModel) {
    return null
  }
  return await getModelById(userSettings.defaultChatModel)
}

export const getSummaryModel = async (userId: string) => {
  const userSettings = await getSettings(userId)
  if (!userSettings?.defaultSummaryModel) {
    return null
  }
  return await getModelById(userSettings.defaultSummaryModel)
}

export const getEmbeddingModel = async (userId: string) => {
  const userSettings = await getSettings(userId)
  if (!userSettings?.defaultEmbeddingModel) {
    return null
  }
  return await getModelById(userSettings.defaultEmbeddingModel)
}