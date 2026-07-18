/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/
import { api, type ApiRequestConfig } from '@/lib/api'
import { getTurnstileHeaders } from '@/features/auth/lib/turnstile-request'
import type {
  ApiResponse,
  UserProfile,
  UpdateUserRequest,
  UpdateUserSettingsRequest,
  DeleteAccountRequest,
  CheckinStatusResponse,
  CheckinResponse,
} from './types'

const sensitiveActionConfig: ApiRequestConfig = {
  skipBusinessError: true,
  skipErrorHandler: true,
}

// ============================================================================
// User Profile APIs
// ============================================================================

/**
 * Get current user profile
 */
export async function getUserProfile(): Promise<ApiResponse<UserProfile>> {
  const res = await api.get('/api/user/self')
  return res.data
}

/**
 * Update user profile
 */
export async function updateUserProfile(
  data: UpdateUserRequest
): Promise<ApiResponse> {
  const res = await api.put('/api/user/self', data)
  return res.data
}

/**
 * Update user settings
 */
export async function updateUserSettings(
  data: UpdateUserSettingsRequest
): Promise<ApiResponse> {
  const res = await api.put('/api/user/setting', data)
  return res.data
}

/**
 * Update interface language preference
 */
export async function updateUserLanguage(
  language: string
): Promise<ApiResponse> {
  const res = await api.put('/api/user/self', { language })
  return res.data
}

/**
 * Delete user account
 */
export async function deleteUserAccount(
  data?: DeleteAccountRequest
): Promise<ApiResponse> {
  const res = await api.delete('/api/user/self', { data })
  return res.data
}

/**
 * Generate/regenerate system access token
 */
export async function generateAccessToken(): Promise<ApiResponse<string>> {
  const res = await api.post<ApiResponse<string>>(
    '/api/user/token',
    undefined,
    sensitiveActionConfig
  )
  return res.data
}

// ============================================================================
// Account Binding APIs
// ============================================================================

/**
 * Send email verification code
 */
export async function sendEmailVerification(
  email: string,
  turnstileToken?: string
): Promise<ApiResponse> {
  const res = await api.get('/api/verification', {
    params: { email },
    headers: getTurnstileHeaders(turnstileToken),
  })
  return res.data
}

/**
 * Bind email account
 */
export async function bindEmail(
  email: string,
  code: string
): Promise<ApiResponse> {
  const res = await api.post('/api/oauth/email/bind', {
    email,
    code,
  })
  return res.data
}

/**
 * Bind WeChat account
 */
export async function bindWeChat(code: string): Promise<ApiResponse> {
  const res = await api.get(`/api/oauth/wechat/bind?code=${code}`)
  return res.data
}

// ============================================================================
// Custom OAuth Binding APIs
// ============================================================================

export interface CustomOAuthBinding {
  provider_id: string
  provider_name: string
  external_id?: string
}

/**
 * Get current user's custom OAuth bindings
 */
export async function getSelfOAuthBindings(): Promise<
  ApiResponse<CustomOAuthBinding[]>
> {
  const res = await api.get('/api/user/oauth/bindings')
  return res.data
}

/**
 * Unbind a custom OAuth provider for current user
 */
export async function unbindCustomOAuth(
  providerId: string
): Promise<ApiResponse> {
  const res = await api.delete(`/api/user/oauth/bindings/${providerId}`)
  return res.data
}

// ============================================================================
// Checkin APIs
// ============================================================================

/**
 * Get checkin status for a specific month
 */
export async function getCheckinStatus(
  month: string
): Promise<ApiResponse<CheckinStatusResponse>> {
  const res = await api.get(`/api/user/checkin?month=${month}`)
  return res.data
}

/**
 * Perform daily checkin
 */
export async function performCheckin(
  turnstileToken?: string
): Promise<ApiResponse<CheckinResponse>> {
  const res = await api.post('/api/user/checkin', undefined, {
    headers: getTurnstileHeaders(turnstileToken),
  })
  return res.data
}
