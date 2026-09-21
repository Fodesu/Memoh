import { normalizeAgentID } from '@/utils/external-agent'
import { safeSessionGet, safeSessionRemove, safeSessionSet } from '@/utils/safe-storage'
import { ONBOARDING_KEYS } from './constants'

const LEGACY_SESSION_KEYS = [
  'memoh:onboarding:runtime-state',
  'memoh:onboarding:acp-selection',
  'memoh:onboarding:created-bot-id',
  'memoh:onboarding:provider-added-count',
  // Replaced by the server-side `initial_bot_id` user metadata plus the
  // bot-less handoff below.
  'memoh:onboarding:bot-result',
] as const

export interface OnboardingAgentResult {
  authorizationId?: string
  agentId: string
  botAgentId: string
}

// OnboardingHandoff carries UI-only hints between the Bot step and the
// completion step. The created Bot itself is recorded server-side as the
// user's `initial_bot_id`, so this never holds a Bot id.
export interface OnboardingHandoff {
  modelConfigured: boolean
  agent?: OnboardingAgentResult
}

let providerIdMemory: string | undefined
let handoffMemory: OnboardingHandoff | null | undefined

function normalizeProviderId(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function normalizeHandoff(value: unknown): OnboardingHandoff | null {
  if (!value || typeof value !== 'object') return null
  const candidate = value as Partial<OnboardingHandoff>

  const agentId = normalizeAgentID(candidate.agent?.agentId)
  const botAgentId = normalizeProviderId(candidate.agent?.botAgentId)

  return {
    modelConfigured: candidate.modelConfigured === true,
    ...(agentId && (botAgentId || agentId === 'codex' || agentId === 'claude-code') && {
      agent: {
        agentId,
        botAgentId,
        authorizationId: normalizeProviderId(candidate.agent?.authorizationId) || undefined,
      },
    }),
  }
}

export function readOnboardingProviderId(): string {
  if (providerIdMemory !== undefined) return providerIdMemory
  return normalizeProviderId(safeSessionGet(ONBOARDING_KEYS.providerId))
}

export function writeOnboardingProviderId(providerId: string): void {
  providerIdMemory = normalizeProviderId(providerId)
  if (providerIdMemory) {
    safeSessionSet(ONBOARDING_KEYS.providerId, providerIdMemory)
  } else {
    safeSessionRemove(ONBOARDING_KEYS.providerId)
  }
}

export function readOnboardingHandoff(): OnboardingHandoff | null {
  if (handoffMemory !== undefined) return handoffMemory
  const raw = safeSessionGet(ONBOARDING_KEYS.handoff)
  if (raw) {
    try {
      return normalizeHandoff(JSON.parse(raw))
    } catch {
      return null
    }
  }
  return null
}

export function writeOnboardingHandoff(handoff: OnboardingHandoff): void {
  handoffMemory = normalizeHandoff(handoff)
  if (!handoffMemory) {
    clearOnboardingHandoff()
    return
  }
  safeSessionSet(ONBOARDING_KEYS.handoff, JSON.stringify(handoffMemory))
}

export function clearOnboardingHandoff(): void {
  handoffMemory = null
  safeSessionRemove(ONBOARDING_KEYS.handoff)
}

export function resetOnboardingSession(): void {
  providerIdMemory = ''
  handoffMemory = null
  safeSessionRemove(ONBOARDING_KEYS.providerId)
  safeSessionRemove(ONBOARDING_KEYS.handoff)
  purgeLegacyOnboardingSession()
}

function purgeLegacyOnboardingSession(): void {
  for (const key of LEGACY_SESSION_KEYS) safeSessionRemove(key)
}

// Older onboarding state could include ACP API keys. It has no valid consumer
// after this module replaces those shapes, so purge it eagerly.
purgeLegacyOnboardingSession()
