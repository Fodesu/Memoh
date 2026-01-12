import { createGateway as createAiGateway } from 'ai'
import { createOpenAI } from '@ai-sdk/openai'
import { createAnthropic } from '@ai-sdk/anthropic'
import { createGoogleGenerativeAI } from '@ai-sdk/google'
import { ChatModel, ModelClientType } from '@memoh/shared'

export const createChatGateway = (model: ChatModel) => {
  const clients = {
    [ModelClientType.OPENAI]: createOpenAI,
    [ModelClientType.ANTHROPIC]: createAnthropic,
    [ModelClientType.GOOGLE]: createGoogleGenerativeAI,
  }
  return (clients[model.clientType] ?? createAiGateway)({
    apiKey: model.apiKey,
    baseURL: model.baseUrl,
  })(model.modelId)
}