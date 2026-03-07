import { describe, expect, it } from 'vitest'
import { createImagePartFromAttachment } from './utils/image-parts'

describe('createImagePartFromAttachment', () => {
  it('converts inline data URLs to binary image parts', () => {
    const part = createImagePartFromAttachment({
      type: 'image',
      transport: 'inline_data_url',
      payload: 'data:image/png;base64,AQID',
    })

    expect(part?.type).toBe('image')
    expect(part?.image).toBeInstanceOf(Uint8Array)
    expect(Array.from(part?.image as Uint8Array)).toEqual([1, 2, 3])
    expect(part?.mediaType).toBe('image/png')
  })

  it('keeps public URLs as URL objects', () => {
    const part = createImagePartFromAttachment({
      type: 'image',
      transport: 'public_url',
      payload: 'https://example.com/demo.png',
    })

    expect(part?.image).toBeInstanceOf(URL)
    expect(String(part?.image)).toBe('https://example.com/demo.png')
  })

  it('falls back to string payloads for malformed public URLs', () => {
    const part = createImagePartFromAttachment({
      type: 'image',
      transport: 'public_url',
      payload: 'https://',
      mime: 'image/png',
    })

    expect(part?.image).toBe('https://')
    expect(part?.mediaType).toBe('image/png')
  })

  it('keeps inline payload strings when they are not data URLs', () => {
    const part = createImagePartFromAttachment({
      type: 'image',
      transport: 'inline_data_url',
      payload: 'AQID',
      mime: 'image/png',
    })

    expect(part?.image).toBe('AQID')
    expect(part?.mediaType).toBe('image/png')
  })

  it('falls back to string payloads for malformed non-base64 data URLs', () => {
    const payload = 'data:image/png,a%ZZ'
    const part = createImagePartFromAttachment({
      type: 'image',
      transport: 'inline_data_url',
      payload,
      mime: 'image/png',
    })

    expect(part?.image).toBe(payload)
    expect(part?.mediaType).toBe('image/png')
  })

  it('falls back to string payloads for malformed base64 data URLs', () => {
    const payload = 'data:image/png;base64,%%%'
    const part = createImagePartFromAttachment({
      type: 'image',
      transport: 'inline_data_url',
      payload,
      mime: 'image/png',
    })

    expect(part?.image).toBe(payload)
    expect(part?.mediaType).toBe('image/png')
  })

  it('skips tool file references', () => {
    const part = createImagePartFromAttachment({
      type: 'image',
      transport: 'tool_file_ref',
      payload: '/data/media/demo.png',
    })

    expect(part).toBeNull()
  })
})
