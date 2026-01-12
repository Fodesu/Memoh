import { Memory, type Message } from 'mem0ai/oss'
import { ChatModel, EmbeddingModel } from '@memoh/shared'
import { MemoryUnit } from './memory-unit'
import { db } from '@memoh/db'
import { history } from '@memoh/db/schema'

export interface CreateMemoryParams {
  summaryModel: ChatModel
  embeddingModel: EmbeddingModel
}

export const createMemory = ({ summaryModel, embeddingModel }: CreateMemoryParams) => {
  process.env.OPENAI_BASE_URL = embeddingModel.baseUrl
  const memory = new Memory({
    version: 'v1.1',
    embedder: {
      provider: 'openai',
      config: {
        apiKey: embeddingModel.apiKey,
        model: embeddingModel.modelId,
        embeddingDims: embeddingModel.dimensions,
        url: embeddingModel.baseUrl,
      }
    },
    llm: {
      provider: summaryModel.clientType,
      config: {
        apiKey: summaryModel.apiKey,
        model: summaryModel.modelId,
        baseURL: summaryModel.baseUrl,
      }
    },
    vectorStore: {
      provider: 'qdrant',
      config: {
        collectionName: 'memory',
        embeddingModelDims: embeddingModel.dimensions,
        url: process.env.QDRANT_URL!,
      }
    }
  })

  const addMemory = async (memoryUnit: MemoryUnit) => {
    await memory.add(memoryUnit.messages as Message[], {
      userId: memoryUnit.user,
    })
    await db.insert(history)
      .values({
        id: crypto.randomUUID(),
        timestamp: memoryUnit.timestamp,
        user: memoryUnit.user,
        messages: memoryUnit.messages,
      })
      .onConflictDoNothing()
  }

  const searchMemory = async (query: string, userId: string) => {
    console.log('Searching memory with query:', query, 'userId:', userId)
    try {
    const { results } = await memory.search(query, {
      userId,
    })
    return results.map((result) => ({
      content: result.memory,
        metadata: result.metadata,
      }))
    } catch (error) {
      console.error('Memory search error:', error)
      // Log the full error details if available
      if (error && typeof error === 'object') {
        console.error('Error details:', JSON.stringify(error, null, 2))
      }
      return []
    }
  }

  return {
    addMemory,
    searchMemory,
  }
}