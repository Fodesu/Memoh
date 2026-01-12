import { createMemory, MemoryUnit } from '@memoh/memory'
import { getEmbeddingModel, getSummaryModel } from '@/modules/model/service'
import { ChatModel, EmbeddingModel } from '@memoh/shared'

export const addMemory = async (memoryUnit: MemoryUnit) => {
  const [embeddingModel, summaryModel] = await Promise.all([
    getEmbeddingModel(memoryUnit.user),
    getSummaryModel(memoryUnit.user),
  ])
  if (!embeddingModel || !summaryModel) {
    throw new Error('Embedding or summary model not found')
  }
  const { addMemory } = createMemory({
    summaryModel: summaryModel.model as ChatModel,
    embeddingModel: embeddingModel.model as EmbeddingModel,
  })
  await addMemory(memoryUnit)
  return memoryUnit
}

export const searchMemory = async (query: string, userId: string) => {
  const [embeddingModel, summaryModel] = await Promise.all([
    getEmbeddingModel(userId),
    getSummaryModel(userId),
  ])
  if (!embeddingModel || !summaryModel) {
    throw new Error('Embedding or summary model not found')
  }
  const { searchMemory } = createMemory({
    summaryModel: summaryModel.model as ChatModel,
    embeddingModel: embeddingModel.model as EmbeddingModel,
  })
  const results = await searchMemory(query, userId)
  return results
}