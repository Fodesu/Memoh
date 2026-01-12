import { createOpenAI } from '@ai-sdk/openai'
import { EmbeddingModel } from '@memoh/shared'

export const createEmbeddingGateway = (model: EmbeddingModel) => {
  return createOpenAI({
    apiKey: model.apiKey,
    baseURL: model.baseUrl,
  }).embedding(model.modelId)
}