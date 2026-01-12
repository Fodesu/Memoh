import { db } from '@memoh/db'
import { settings } from '@memoh/db/schema'
import { eq } from 'drizzle-orm'
import type { SettingsInput } from './model'

export const getSettings = async (userId: string) => {
  const [result] = await db
    .select()
    .from(settings)
    .where(eq(settings.userId, userId))
  return result
}

export const upsertSettings = async (userId: string, data: SettingsInput) => {
  const updateData: Record<string, unknown> = {}
  
  if (data.defaultChatModel !== undefined) {
    updateData.defaultChatModel = data.defaultChatModel
  }
  if (data.defaultEmbeddingModel !== undefined) {
    updateData.defaultEmbeddingModel = data.defaultEmbeddingModel
  }
  if (data.defaultSummaryModel !== undefined) {
    updateData.defaultSummaryModel = data.defaultSummaryModel
  }
  if (data.maxContextLoadTime !== undefined) {
    updateData.maxContextLoadTime = data.maxContextLoadTime
  }
  if (data.language !== undefined) {
    updateData.language = data.language
  }

  const [result] = await db
    .insert(settings)
    .values({
      userId: userId,
      defaultChatModel: data.defaultChatModel || null,
      defaultEmbeddingModel: data.defaultEmbeddingModel || null,
      defaultSummaryModel: data.defaultSummaryModel || null,
      maxContextLoadTime: data.maxContextLoadTime || 60,
      language: data.language || 'Same as user input',
    })
    .onConflictDoUpdate({
      target: settings.userId,
      set: updateData,
    })
    .returning()
  return result
}

