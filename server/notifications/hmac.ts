import { createHmac } from 'node:crypto'

/**
 * Computes HMAC-SHA256. Payload format: `${timestamp}.${rawBody}`.
 * Returns hex digest (callers prepend `sha256=`).
 */
export function computeWebhookHmac(secret: string, payload: string): string {
  return createHmac('sha256', secret).update(payload).digest('hex')
}

/**
 * Builds the full value for the X-Dashboard-Signature header.
 */
export function buildSignatureHeader(secret: string, timestamp: string, rawBody: string): string {
  const sig = computeWebhookHmac(secret, `${timestamp}.${rawBody}`)
  return `sha256=${sig}`
}
