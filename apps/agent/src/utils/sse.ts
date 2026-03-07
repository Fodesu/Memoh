export const defaultSSEChunkSize = 16 * 1024

export function sseChunked(data: string, chunkSize: number = defaultSSEChunkSize) {
  return {
    sse: true as const,
    toSSE: () => {
      const out: string[] = []
      for (const chunk of chunkString(data, chunkSize)) {
        out.push(`data:${chunk}\n`)
      }
      out.push('\n')
      return out.join('')
    },
  }
}

export function* chunkString(input: string, maxLen: number): Generator<string> {
  if (maxLen <= 0) {
    yield input
    return
  }
  const isHighSurrogate = (c: number) => c >= 0xd800 && c <= 0xdbff
  const isLowSurrogate = (c: number) => c >= 0xdc00 && c <= 0xdfff
  let i = 0
  while (i < input.length) {
    let end = Math.min(i + maxLen, input.length)
    if (end < input.length) {
      const last = input.charCodeAt(end - 1)
      if (isHighSurrogate(last)) {
        const next = input.charCodeAt(end)
        if (isLowSurrogate(next)) {
          end += 1
        } else {
          end -= 1
        }
      }
    }
    if (end <= i) {
      const first = input.charCodeAt(i)
      const second = i+1 < input.length ? input.charCodeAt(i + 1) : -1
      if (isHighSurrogate(first) && isLowSurrogate(second)) {
        end = Math.min(i + 2, input.length)
      } else {
        end = Math.min(i + 1, input.length)
      }
    }
    yield input.slice(i, end)
    i = end
  }
}
