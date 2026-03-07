import type { ImagePart } from 'ai'
import type { GatewayInputAttachment } from '../types/attachment'

type NativeImageAttachment = GatewayInputAttachment & {
  type: 'image'
  transport: 'inline_data_url' | 'public_url'
}

type ImagePartPayload = string | Uint8Array | URL
const strictBase64Pattern = /^[A-Za-z0-9+/]*={0,2}$/

const normalizeMediaType = (value?: string): string | undefined => {
  const mediaType = typeof value === 'string' ? value.trim() : ''
  return mediaType || undefined
}

const createImagePart = (image: ImagePartPayload, mediaType?: string): ImagePart => {
  const normalizedMediaType = normalizeMediaType(mediaType)
  if (normalizedMediaType == null) {
    return { type: 'image', image }
  }
  return { type: 'image', image, mediaType: normalizedMediaType }
}

const decodeBase64Strict = (value: string): Buffer | null => {
  const normalized = value.replace(/\s+/g, '')
  if (normalized === '' || !strictBase64Pattern.test(normalized)) {
    return null
  }

  const firstPadding = normalized.indexOf('=')
  if (firstPadding >= 0) {
    if (/[A-Za-z0-9+/]/.test(normalized.slice(firstPadding))) {
      return null
    }
    if (normalized.length-firstPadding > 2 || normalized.length % 4 !== 0) {
      return null
    }
  }
  else if (normalized.length % 4 === 1) {
    return null
  }

  const padded = firstPadding >= 0
    ? normalized
    : normalized + '='.repeat((4 - (normalized.length % 4)) % 4)

  const decoded = Buffer.from(padded, 'base64')
  const canonical = decoded.toString('base64').replace(/=+$/g, '')
  const input = normalized.replace(/=+$/g, '')
  if (canonical !== input) {
    return null
  }

  return decoded
}

const parseDataUrl = (payload: string): { bytes: Uint8Array; mediaType?: string } | null => {
  const trimmed = payload.trim()
  if (!trimmed.toLowerCase().startsWith('data:')) {
    return null
  }

  const commaIndex = trimmed.indexOf(',')
  if (commaIndex < 0) {
    return null
  }

  const header = trimmed.slice(5, commaIndex)
  const body = trimmed.slice(commaIndex + 1)
  const segments = header.split(';').map((segment) => segment.trim()).filter(Boolean)
  const mediaType = normalizeMediaType(segments.find((segment) => segment.includes('/')))
  const isBase64 = segments.some((segment) => segment.toLowerCase() === 'base64')
  let buffer: Buffer
  if (isBase64) {
    const decoded = decodeBase64Strict(body)
    if (decoded == null) {
      return null
    }
    buffer = decoded
  }
  else {
    try {
      buffer = Buffer.from(decodeURIComponent(body), 'utf8')
    }
    catch {
      return null
    }
  }

  return {
    bytes: new Uint8Array(buffer),
    mediaType,
  }
}

const isNativeImageAttachment = (
  attachment: GatewayInputAttachment,
): attachment is NativeImageAttachment => {
  if (attachment.type !== 'image') {
    return false
  }
  if (attachment.transport !== 'inline_data_url' && attachment.transport !== 'public_url') {
    return false
  }
  return typeof attachment.payload === 'string' && attachment.payload.trim() !== ''
}

const createInlineDataImagePart = (payload: string, mediaType?: string): ImagePart => {
  const parsed = parseDataUrl(payload)
  if (parsed != null) {
    return createImagePart(parsed.bytes, mediaType ?? parsed.mediaType)
  }
  return createImagePart(payload, mediaType)
}

const createPublicURLImagePart = (payload: string, mediaType?: string): ImagePart => {
  try {
    return createImagePart(new URL(payload), mediaType)
  }
  catch {
    return createImagePart(payload, mediaType)
  }
}

export const createBinaryImagePart = (bytes: Uint8Array, mediaType?: string): ImagePart => {
  return createImagePart(bytes, mediaType)
}

export const createImagePartFromAttachment = (
  attachment: GatewayInputAttachment,
): ImagePart | null => {
  if (!isNativeImageAttachment(attachment)) {
    return null
  }

  const payload = attachment.payload.trim()
  switch (attachment.transport) {
    case 'public_url':
      return createPublicURLImagePart(payload, attachment.mime)
    case 'inline_data_url':
      return createInlineDataImagePart(payload, attachment.mime)
  }
}
