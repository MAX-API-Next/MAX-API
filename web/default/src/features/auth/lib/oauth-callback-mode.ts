import { z } from 'zod'
import { isOAuthBindingState } from '@/lib/oauth'

export type OAuthCallbackMode = 'login' | 'bind'

const oauthBindSearchSchema = z
  .union([z.boolean(), z.enum(['true', 'false'])])
  .transform((value) => value === true || value === 'true')

export const oauthCallbackSearchSchema = z.object({
  code: z.string().optional().catch(undefined),
  state: z.string().optional().catch(undefined),
  redirect: z.string().optional().catch(undefined),
  bind: oauthBindSearchSchema.optional().catch(undefined),
})

export function resolveOAuthCallbackMode(
  provider?: string,
  state?: string,
  bindHint = false
): OAuthCallbackMode {
  return bindHint || isOAuthBindingState(provider, state) ? 'bind' : 'login'
}
