import { isOAuthBindingState } from '@/lib/oauth'

export type OAuthCallbackMode = 'login' | 'bind'

export function resolveOAuthCallbackMode(
  provider?: string,
  state?: string,
  bindHint = false
): OAuthCallbackMode {
  return bindHint || isOAuthBindingState(provider, state) ? 'bind' : 'login'
}
