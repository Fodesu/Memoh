import { computed, effectScope, nextTick, ref, type Ref } from 'vue'
import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const postDisplayPrepareStream = vi.fn()
const getDisplay = vi.fn()
const deleteDisplaySession = vi.fn()
const localStorageStub = {
  getItem: (key: string) => key === 'language' ? 'zh' : null,
  setItem: vi.fn(),
  removeItem: vi.fn(),
  clear: vi.fn(),
}

vi.stubGlobal('localStorage', localStorageStub)

vi.mock('@memohai/sdk', () => ({
  deleteBotsByBotIdContainerDisplaySessionsBySessionId: (...args: unknown[]) => deleteDisplaySession(...args),
  getBotsByBotIdContainerDisplay: (...args: unknown[]) => getDisplay(...args),
  postBotsByBotIdContainerDisplayWebrtcOffer: vi.fn(),
}))

vi.mock('@/composables/api/useDisplayPrepareStream', () => ({
  postBotsByBotIdContainerDisplayPrepareStream: (...args: unknown[]) => postDisplayPrepareStream(...args),
}))

const { BotDisplayConnection, useBotDisplayConnection } = await import('./useBotDisplayConnection')

describe('BotDisplayConnection structured preparation errors', () => {
  afterAll(() => {
    vi.unstubAllGlobals()
  })

  beforeEach(() => {
    postDisplayPrepareStream.mockReset()
    getDisplay.mockReset()
    vi.stubGlobal('localStorage', localStorageStub)
  })

  it('renders a structured preparation error through the public readiness flow', async () => {
    getDisplay.mockResolvedValue({
      data: {
        enabled: true,
        available: false,
        running: false,
        prepare_supported: true,
      },
    })
    postDisplayPrepareStream.mockResolvedValue({
      stream: (async function* () {
        yield {
          type: 'error',
          code: 'workspace.unreachable',
          args: {},
          message: 'The workspace could not be reached.',
          request_id: 'req-display-1',
        }
      })(),
    })

    const connection = new BotDisplayConnection('bot-1')
    const ready = await connection.ensureReady()

    expect(ready).toBe(false)
    expect(connection.status.value).toBe('unavailable')
    expect(connection.unavailableReason.value).toBe('暂时无法连接工作区，请稍后重试。')
  })
})

describe('BotDisplayConnection transport recovery', () => {
  class Channel extends EventTarget {
    readyState = 'open'
    send = vi.fn()
    close() {
      if (this.readyState === 'closed') return
      this.readyState = 'closed'
      this.dispatchEvent(new Event('close'))
    }
  }
  class Peer extends EventTarget {
    connectionState: RTCPeerConnectionState = 'new'
    channel = new Channel()
    createDataChannel() { return this.channel }
    addTransceiver() {}
    close = vi.fn(() => {
      this.connectionState = 'closed'
      this.dispatchEvent(new Event('connectionstatechange'))
    })
  }

  const cleanups: (() => void)[] = []

  beforeEach(() => {
    vi.clearAllMocks()
    deleteDisplaySession.mockResolvedValue({})
    vi.useFakeTimers()
    vi.stubGlobal('window', { setTimeout: (callback: () => void, ms: number) => setTimeout(callback, ms), clearTimeout: (timer: number) => clearTimeout(timer) })
    vi.stubGlobal('RTCPeerConnection', Peer)
    getDisplay.mockResolvedValue({ data: { enabled: true, available: true, running: true } })
    vi.spyOn(BotDisplayConnection.prototype, 'exchangeOffer').mockResolvedValue({ type: 'answer', sdp: 'stub', session_id: 'viewer' })
  })

  afterEach(() => {
    for (const cleanup of cleanups.splice(0)) cleanup()
    vi.restoreAllMocks()
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  async function connected() {
    const connection = new BotDisplayConnection('transport-test')
    connection.addRef()
    cleanups.push(() => connection.removeRef())
    await connection.acquireConnection()
    const peer = connection.peer.value as unknown as Peer
    peer.connectionState = 'connected'
    peer.dispatchEvent(new Event('connectionstatechange'))
    connection.stream.value = {} as MediaStream
    return { connection, peer }
  }

  it('clears the dead stream and peer when the input transport closes', async () => {
    const { connection, peer } = await connected()
    peer.channel.close()
    expect(connection.status.value).toBe('disconnected')
    expect(connection.stream.value).toBeNull()
    expect(connection.inputChannel.value).toBeNull()
    expect(connection.peer.value).toBeNull()
    expect(peer.close).toHaveBeenCalledOnce()
  })

  it('does not reuse a connected peer whose input channel is dead', async () => {
    const { connection, peer } = await connected()
    peer.channel.readyState = 'closed'
    await connection.acquireConnection()
    expect(connection.peer.value).not.toBe(peer)
    expect(peer.close).toHaveBeenCalledOnce()
  })

  it('ignores late state and track events from a replaced peer', async () => {
    const { connection, peer } = await connected()
    peer.channel.close()
    await connection.acquireConnection()
    const current = connection.peer.value
    peer.connectionState = 'failed'
    peer.dispatchEvent(new Event('connectionstatechange'))
    peer.channel.dispatchEvent(new Event('close'))
    const event = new Event('track')
    Object.assign(event, { streams: [{}] })
    peer.dispatchEvent(event)
    expect(connection.peer.value).toBe(current)
    expect(connection.status.value).toBe('connecting')
    expect(connection.stream.value).toBeNull()
  })

  it('automatically reacquires a transport lost by a mounted viewer', async () => {
    const { connection, peer } = await connected()
    peer.channel.close()
    await vi.advanceTimersByTimeAsync(1000)
    expect(connection.peer.value).not.toBeNull()
    expect(connection.peer.value).not.toBe(peer)
    expect(connection.status.value).toBe('connecting')
  })

  it.each(['disconnected', 'failed', 'closed'] as const)('recovers when the peer becomes %s before the channel closes', async (state) => {
    const { connection, peer } = await connected()
    peer.connectionState = state
    peer.dispatchEvent(new Event('connectionstatechange'))
    peer.channel.dispatchEvent(new Event('close'))
    expect(connection.stream.value).toBeNull()
    await vi.advanceTimersByTimeAsync(1000)
    expect(getDisplay).toHaveBeenCalledTimes(2)
    expect(connection.peer.value).not.toBeNull()
    expect(connection.peer.value).not.toBe(peer)
  })

  it('keeps retrying temporary readiness and offer failures with backoff', async () => {
    const { connection, peer } = await connected()
    getDisplay.mockRejectedValueOnce(new Error('temporarily offline'))
    vi.mocked(BotDisplayConnection.prototype.exchangeOffer).mockRejectedValueOnce(new Error('offer interrupted'))
    peer.channel.close()
    await vi.advanceTimersByTimeAsync(1000)
    expect(getDisplay).toHaveBeenCalledTimes(2)
    expect(connection.status.value).toBe('unavailable')
    await vi.advanceTimersByTimeAsync(1999)
    expect(getDisplay).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1)
    expect(getDisplay).toHaveBeenCalledTimes(3)
    expect(connection.peer.value).toBeNull()
    await vi.advanceTimersByTimeAsync(4000)
    expect(getDisplay).toHaveBeenCalledTimes(4)
    const recovered = connection.peer.value as unknown as Peer
    expect(recovered).not.toBeNull()
    recovered.connectionState = 'connected'
    recovered.dispatchEvent(new Event('connectionstatechange'))
    recovered.channel.close()
    await vi.advanceTimersByTimeAsync(1000)
    expect(getDisplay).toHaveBeenCalledTimes(5)
  })

  it('stops recovery after a failed retry when the final viewer hides', async () => {
    const visible = ref(true)
    const { connection } = viewer('hide-during-recovery', visible)
    await connection.acquireConnection()
    const peer = connection.peer.value as unknown as Peer
    peer.connectionState = 'connected'
    peer.dispatchEvent(new Event('connectionstatechange'))
    getDisplay.mockRejectedValue(new Error('offline'))
    peer.channel.close()
    await vi.advanceTimersByTimeAsync(1000)
    expect(getDisplay).toHaveBeenCalledTimes(2)
    visible.value = false
    await nextTick()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(getDisplay).toHaveBeenCalledTimes(2)
    expect(connection.status.value).toBe('idle')
  })

  function viewer(botId: string, visible: Ref<boolean>) {
    const scope = effectScope()
    const connection = scope.run(() => useBotDisplayConnection(botId, visible))!
    cleanups.push(() => scope.stop())
    return { connection, stop: () => scope.stop() }
  }

  function deferred<T>() {
    let resolve!: (value: T) => void
    const promise = new Promise<T>((done) => { resolve = done })
    return { promise, resolve }
  }

  it('waits for display preparation before creating a WebRTC peer', async () => {
    const info = deferred<{ data: { enabled: boolean; available: boolean; running: boolean } }>()
    getDisplay.mockReturnValueOnce(info.promise)
    const { connection } = viewer('preparation-order', ref(true))
    const acquisition = connection.acquireConnection()
    expect(connection.status.value).toBe('connecting')
    expect(connection.peer.value).toBeNull()
    expect(BotDisplayConnection.prototype.exchangeOffer).not.toHaveBeenCalled()
    info.resolve({ data: { enabled: true, available: true, running: true } })
    expect(await acquisition).toBe(true)
    expect(connection.peer.value).not.toBeNull()
  })

  it('acquires only while both the panel and browser document are visible', async () => {
    const panel = ref(false)
    const documentVisible = ref(true)
    const visible = computed(() => panel.value && documentVisible.value)
    const { connection } = viewer('visibility', visible)
    expect(getDisplay).not.toHaveBeenCalled()
    expect(await connection.acquireConnection()).toBe(false)

    panel.value = true
    await nextTick()
    await connection.acquireConnection()
    const first = connection.peer.value as unknown as Peer
    expect(first).not.toBeNull()
    documentVisible.value = false
    await nextTick()
    expect(first.close).toHaveBeenCalledOnce()
    expect(connection.peer.value).toBeNull()
    expect(connection.status.value).toBe('idle')
    expect(deleteDisplaySession).toHaveBeenCalledWith(expect.objectContaining({
      path: { bot_id: 'visibility', session_id: 'viewer' },
    }))
    await vi.advanceTimersByTimeAsync(60_000)
    expect(getDisplay).toHaveBeenCalledTimes(1)

    documentVisible.value = true
    await nextTick()
    await connection.acquireConnection()
    expect(connection.peer.value).not.toBe(first)
    panel.value = false
    await nextTick()
    expect(connection.peer.value).toBeNull()
    expect(deleteDisplaySession).toHaveBeenCalledTimes(2)
  })

  it('keeps one shared peer until the final visible viewer hides', async () => {
    const firstVisible = ref(true)
    const secondVisible = ref(true)
    const first = viewer('shared', firstVisible)
    const second = viewer('shared', secondVisible)
    expect(first.connection).toBe(second.connection)
    await first.connection.acquireConnection()
    const peer = first.connection.peer.value as unknown as Peer
    expect(getDisplay).toHaveBeenCalledTimes(1)
    firstVisible.value = false
    await nextTick()
    expect(first.connection.peer.value).toBe(peer)
    first.stop()
    expect(peer.close).not.toHaveBeenCalled()
    secondVisible.value = false
    await nextTick()
    expect(peer.close).toHaveBeenCalledOnce()

    const third = viewer('shared', ref(true))
    expect(third.connection).toBe(second.connection)
    await third.connection.acquireConnection()
    second.stop()
    expect(third.connection.peer.value).not.toBeNull()
  })

  it('cancels the reconnect timer when the last viewer hides', async () => {
    const visible = ref(true)
    const { connection } = viewer('cancel-reconnect', visible)
    await connection.acquireConnection()
    const peer = connection.peer.value as unknown as Peer
    peer.connectionState = 'connected'
    peer.dispatchEvent(new Event('connectionstatechange'))
    peer.channel.close()
    visible.value = false
    await nextTick()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(getDisplay).toHaveBeenCalledTimes(1)
    expect(connection.peer.value).toBeNull()
    expect(connection.status.value).toBe('idle')
  })

  it('aborts preparation and ignores its late result after a viewer returns', async () => {
    const oldInfo = deferred<{ data: { enabled: boolean } }>()
    getDisplay.mockReturnValueOnce(oldInfo.promise)
    const visible = ref(true)
    const { connection } = viewer('cancel-prepare', visible)
    const oldAcquisition = connection.acquireConnection()
    const oldSignal = getDisplay.mock.calls[0]![0].signal as AbortSignal
    visible.value = false
    await nextTick()
    expect(oldSignal.aborted).toBe(true)
    visible.value = true
    await nextTick()
    await connection.acquireConnection()
    const current = connection.peer.value
    oldInfo.resolve({ data: { enabled: false } })
    expect(await oldAcquisition).toBe(false)
    expect(connection.peer.value).toBe(current)
    expect(connection.status.value).toBe('connecting')
    expect(connection.unavailableReason.value).toBe('')
    expect(getDisplay).toHaveBeenCalledTimes(2)
  })

  it('releases a late offer session without touching the new acquisition', async () => {
    const oldAnswer = deferred<{ type: 'answer'; sdp: string; session_id: string }>()
    const offerStarted = deferred<void>()
    vi.mocked(BotDisplayConnection.prototype.exchangeOffer).mockImplementationOnce(() => {
      offerStarted.resolve()
      return oldAnswer.promise
    })
    const visible = ref(true)
    const { connection } = viewer('cancel-offer', visible)
    const oldAcquisition = connection.acquireConnection()
    await offerStarted.promise
    expect(connection.peer.value).not.toBeNull()
    visible.value = false
    await nextTick()
    visible.value = true
    await nextTick()
    await connection.acquireConnection()
    const current = connection.peer.value
    oldAnswer.resolve({ type: 'answer', sdp: 'stub', session_id: 'old-viewer' })
    expect(await oldAcquisition).toBe(false)
    expect(connection.peer.value).toBe(current)
    expect(connection.sessionId.value).toBe('viewer')
    expect(deleteDisplaySession).toHaveBeenCalledWith(expect.objectContaining({
      path: { bot_id: 'cancel-offer', session_id: 'old-viewer' },
    }))
  })

  it('does not let an old completion clear the next pending acquisition', async () => {
    const oldInfo = deferred<{ data: { enabled: boolean } }>()
    const nextInfo = deferred<{ data: { enabled: boolean; available: boolean; running: boolean } }>()
    getDisplay.mockReturnValueOnce(oldInfo.promise).mockReturnValueOnce(nextInfo.promise)
    const visible = ref(true)
    const { connection } = viewer('pending-acquisition', visible)
    const oldAcquisition = connection.acquireConnection()
    visible.value = false
    await nextTick()
    visible.value = true
    await nextTick()
    const currentAcquisition = connection.acquireConnection()
    oldInfo.resolve({ data: { enabled: false } })
    await oldAcquisition
    expect(connection.acquireConnection()).toBe(currentAcquisition)
    nextInfo.resolve({ data: { enabled: true, available: true, running: true } })
    expect(await currentAcquisition).toBe(true)
    expect(getDisplay).toHaveBeenCalledTimes(2)
  })

})
