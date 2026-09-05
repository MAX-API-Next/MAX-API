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
export const H3_BILLING_PROFILE_KEY = 'minimax_h3_v2'

export type H3Resolution = '768P' | '2K'

export type H3BillingProfile = {
  schema_version: number
  mode: string
  currency: string
  output_unit_price: Record<H3Resolution, string>
  input_video_unit_price: Record<H3Resolution, string>
  input_video_max_seconds: number
  input_image_free_count: number
  input_image_extra_unit_price: string
  input_audio_unit_price: string
}

export type H3BillingProfiles = Record<string, H3BillingProfile>

export type H3BillingFormValues = {
  output768Price: string
  output2KPrice: string
  inputVideo768Price: string
  inputVideo2KPrice: string
  inputVideoMaxSeconds: number
  inputImageFreeCount: number
  inputImageExtraPrice: string
}

export type H3BillingPreviewScenario = {
  resolution: H3Resolution
  outputDurationSeconds: number
  inputVideoCount: number
  inputAudioCount: number
  inputImageCount: number
  actual?: {
    outputDurationMs: number
    inputVideoDurationMs?: number
    inputAudioDurationMs?: number
    inputImageCount: number
  }
}

export type H3BillingQuoteComponent = {
  key: string
  unit: string
  quantity: number
  quantity_decimal?: string
  unit_price: string
  price: string
}

export type H3BillingQuote = {
  stage: string
  price: string
  quota: number
  output_seconds: number
  input_video_seconds: number
  input_audio_seconds: number
  output_duration_ms: number
  input_video_duration_ms: number
  input_audio_duration_ms: number
  input_image_count: number
  components: H3BillingQuoteComponent[]
}

export type H3BillingPreview = {
  config_hash: string
  quota_per_unit: number
  group_ratio: number
  estimate: H3BillingQuote
  reserve: H3BillingQuote
  final?: H3BillingQuote
  adjustment_quota?: number
  refund_quota?: number
}

export const DEFAULT_H3_BILLING_PROFILE: H3BillingProfile = {
  schema_version: 1,
  mode: 'bounded_actual',
  currency: 'USD',
  output_unit_price: { '768P': '0.08', '2K': '0.13' },
  input_video_unit_price: { '768P': '0.08', '2K': '0.13' },
  input_video_max_seconds: 15,
  input_image_free_count: 5,
  input_image_extra_unit_price: '0.04',
  input_audio_unit_price: '0',
}

export const DEFAULT_H3_BILLING_PROFILES: H3BillingProfiles = {
  [H3_BILLING_PROFILE_KEY]: DEFAULT_H3_BILLING_PROFILE,
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function isProfile(value: unknown): value is H3BillingProfile {
  if (!isRecord(value)) return false
  const output = value.output_unit_price
  const inputVideo = value.input_video_unit_price
  return (
    typeof value.schema_version === 'number' &&
    typeof value.mode === 'string' &&
    typeof value.currency === 'string' &&
    isRecord(output) &&
    typeof output['768P'] === 'string' &&
    typeof output['2K'] === 'string' &&
    isRecord(inputVideo) &&
    typeof inputVideo['768P'] === 'string' &&
    typeof inputVideo['2K'] === 'string' &&
    typeof value.input_video_max_seconds === 'number' &&
    typeof value.input_image_free_count === 'number' &&
    typeof value.input_image_extra_unit_price === 'string' &&
    typeof value.input_audio_unit_price === 'string'
  )
}

export function parseH3BillingProfiles(value: string): {
  profiles: H3BillingProfiles
  error?: string
} {
  if (!value.trim()) {
    return { profiles: cloneProfiles(DEFAULT_H3_BILLING_PROFILES) }
  }

  try {
    const parsed: unknown = JSON.parse(value)
    if (!isRecord(parsed)) {
      return {
        profiles: cloneProfiles(DEFAULT_H3_BILLING_PROFILES),
        error: 'H3 billing profiles must be a JSON object',
      }
    }
    const profiles: H3BillingProfiles = {}
    for (const [key, profile] of Object.entries(parsed)) {
      if (!isProfile(profile)) {
        return {
          profiles: cloneProfiles(DEFAULT_H3_BILLING_PROFILES),
          error: `H3 billing profile ${key} is incomplete`,
        }
      }
      profiles[key] = cloneProfile(profile)
    }
    if (!profiles[H3_BILLING_PROFILE_KEY]) {
      return {
        profiles: cloneProfiles(DEFAULT_H3_BILLING_PROFILES),
        error: `H3 billing profile ${H3_BILLING_PROFILE_KEY} is required`,
      }
    }
    return { profiles }
  } catch (error) {
    return {
      profiles: cloneProfiles(DEFAULT_H3_BILLING_PROFILES),
      error: error instanceof Error ? error.message : 'Invalid H3 billing JSON',
    }
  }
}

export function buildH3BillingFormValues(
  profile: H3BillingProfile
): H3BillingFormValues {
  return {
    output768Price: profile.output_unit_price['768P'],
    output2KPrice: profile.output_unit_price['2K'],
    inputVideo768Price: profile.input_video_unit_price['768P'],
    inputVideo2KPrice: profile.input_video_unit_price['2K'],
    inputVideoMaxSeconds: profile.input_video_max_seconds,
    inputImageFreeCount: profile.input_image_free_count,
    inputImageExtraPrice: profile.input_image_extra_unit_price,
  }
}

export function buildH3BillingProfile(
  current: H3BillingProfile,
  values: H3BillingFormValues
): H3BillingProfile {
  return {
    ...cloneProfile(current),
    output_unit_price: {
      ...current.output_unit_price,
      '768P': values.output768Price.trim(),
      '2K': values.output2KPrice.trim(),
    },
    input_video_unit_price: {
      ...current.input_video_unit_price,
      '768P': values.inputVideo768Price.trim(),
      '2K': values.inputVideo2KPrice.trim(),
    },
    input_video_max_seconds: values.inputVideoMaxSeconds,
    input_image_free_count: values.inputImageFreeCount,
    input_image_extra_unit_price: values.inputImageExtraPrice.trim(),
    input_audio_unit_price: '0',
  }
}

export function serializeH3BillingProfiles(profiles: H3BillingProfiles) {
  return JSON.stringify(profiles)
}

export function fingerprintH3BillingProfile(profile: H3BillingProfile) {
  return JSON.stringify(profile)
}

export function hasLegacyH3RateCard(value: string) {
  try {
    const parsed: unknown = JSON.parse(value)
    return (
      isRecord(parsed) &&
      Object.keys(parsed).some((key) =>
        key.toLowerCase().includes('minimax-h3')
      )
    )
  } catch {
    return false
  }
}

function cloneProfile(profile: H3BillingProfile): H3BillingProfile {
  return {
    ...profile,
    output_unit_price: { ...profile.output_unit_price },
    input_video_unit_price: { ...profile.input_video_unit_price },
  }
}

function cloneProfiles(profiles: H3BillingProfiles): H3BillingProfiles {
  return Object.fromEntries(
    Object.entries(profiles).map(([key, profile]) => [
      key,
      cloneProfile(profile),
    ])
  )
}
