// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  replace: vi.fn(),
  updateMe: vi.fn(),
  toastError: vi.fn(),
  user: { onboardingCompleted: false, initialBotId: '' },
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ replace: mocks.replace }),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@memohai/sdk', () => ({
  putUsersMe: mocks.updateMe,
}))

vi.mock('@felinic/ui', () => ({
  toast: { error: mocks.toastError },
}))

vi.mock('@/store/user', () => ({
  useUserStore: () => mocks.user,
}))

const storage = new Map<string, string>()

Object.defineProperty(globalThis, 'localStorage', {
  value: {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
    removeItem: (key: string) => storage.delete(key),
    clear: () => storage.clear(),
  },
  configurable: true,
})

import { resetOnboardingState, useOnboarding } from './useOnboarding'
import {
  readOnboardingHandoff,
  resetOnboardingSession,
  writeOnboardingHandoff,
} from '@/pages/onboarding/session'
import { ONBOARDING_KEYS } from '@/pages/onboarding/constants'

describe('useOnboarding completion', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.user.onboardingCompleted = false
    mocks.user.initialBotId = 'bot-id'
    mocks.updateMe.mockResolvedValue({})
    mocks.replace.mockResolvedValue(undefined)
    resetOnboardingState()
    resetOnboardingSession()
    localStorage.clear()
  })

  it('restores forced onboarding and keeps the handoff when navigation fails', async () => {
    writeOnboardingHandoff({
      modelConfigured: false,
      agent: { agentId: 'codex', botAgentId: 'agent-id' },
    })
    localStorage.setItem(ONBOARDING_KEYS.forceOnboarding, '1')

    mocks.replace.mockImplementationOnce(async () => {
      expect(localStorage.getItem(ONBOARDING_KEYS.forceOnboarding)).toBeNull()
      throw new Error('navigation failed')
    })

    expect(await useOnboarding().complete()).toBe(false)
    expect(readOnboardingHandoff()?.agent?.agentId).toBe('codex')
    expect(localStorage.getItem(ONBOARDING_KEYS.forceOnboarding)).toBe('1')
    expect(mocks.toastError).toHaveBeenCalledWith('onboarding.complete.navigationFailed')

    mocks.replace.mockResolvedValue(undefined)
    expect(await useOnboarding().complete()).toBe(true)
    expect(mocks.replace).toHaveBeenLastCalledWith({
      name: 'bot',
      params: { botName: 'bot-id' },
      query: { agent: 'codex' },
    })
    expect(readOnboardingHandoff()).toBeNull()
    expect(localStorage.getItem(ONBOARDING_KEYS.forceOnboarding)).toBeNull()
  })

  it('lands on the home page when the server has no initial Bot for the user', async () => {
    mocks.user.initialBotId = ''

    expect(await useOnboarding().complete()).toBe(true)
    expect(mocks.replace).toHaveBeenLastCalledWith('/')
  })

  it('treats a resolved router failure as a failed completion', async () => {
    writeOnboardingHandoff({ modelConfigured: true })
    mocks.replace.mockResolvedValue({ type: 4 })

    expect(await useOnboarding().complete()).toBe(false)
    expect(readOnboardingHandoff()?.modelConfigured).toBe(true)
    expect(mocks.toastError).toHaveBeenCalledWith('onboarding.complete.navigationFailed')
  })
})
