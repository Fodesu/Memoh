export enum ClientType {
  OPENAI = 'openai',
  ANTHROPIC = 'anthropic',
  GOOGLE = 'google',
}

export interface Schedule {
  id: string
  name: string
  description: string
  pattern: string
  maxCalls?: number
  command: string
}

export interface AgentSkill {
  name: string
  description: string
  content: string
}