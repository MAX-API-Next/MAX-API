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
import type { ModelSettings } from '@/features/system-settings/types'
import { safeJsonParse } from '@/features/system-settings/utils/json-parser'

export type PricingMode = 'per-token' | 'per-request'

const NON_NEGATIVE_DECIMAL_PATTERN =
  /^(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$/

export function parseNonNegativeFiniteNumber(
  value: string
): number | undefined {
  const normalized = value.trim()
  if (!normalized || !NON_NEGATIVE_DECIMAL_PATTERN.test(normalized)) {
    return undefined
  }
  const parsed = Number(normalized)
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : undefined
}

export function parsePricingMap(rawMap: string): Record<string, number> {
  const parsed = safeJsonParse<Record<string, unknown>>(rawMap, {
    fallback: {},
    silent: true,
  })
  return Object.fromEntries(
    Object.entries(parsed).filter(
      (entry): entry is [string, number] =>
        typeof entry[1] === 'number' &&
        Number.isFinite(entry[1]) &&
        entry[1] >= 0
    )
  )
}

type PricingFields = {
  price: string
  ratio: string
  cacheRatio: string
  completionRatio: string
  imageRatio: string
  audioRatio: string
  audioCompletionRatio: string
}

type PricingConfig = {
  mode: PricingMode
  fields: PricingFields
  promptPrice: string
  completionPrice: string
  advancedOpen: boolean
}

const EMPTY_PRICING_FIELDS: PricingFields = {
  price: '',
  ratio: '',
  cacheRatio: '',
  completionRatio: '',
  imageRatio: '',
  audioRatio: '',
  audioCompletionRatio: '',
}

const EMPTY_PRICING_CONFIG: PricingConfig = {
  mode: 'per-token',
  fields: EMPTY_PRICING_FIELDS,
  promptPrice: '',
  completionPrice: '',
  advancedOpen: false,
}

function lookupModelRatio(
  rawMap: string,
  modelName: string
): number | undefined {
  return parsePricingMap(rawMap)[modelName]
}

export function readPricingConfig(
  settings: ModelSettings | null,
  modelName: string
): PricingConfig {
  if (!settings || !modelName) return EMPTY_PRICING_CONFIG

  const price = lookupModelRatio(settings.ModelPrice, modelName)
  const ratio = lookupModelRatio(settings.ModelRatio, modelName)
  const cacheRatio = lookupModelRatio(settings.CacheRatio, modelName)
  const completionRatio = lookupModelRatio(settings.CompletionRatio, modelName)
  const imageRatio = lookupModelRatio(settings.ImageRatio, modelName)
  const audioRatio = lookupModelRatio(settings.AudioRatio, modelName)
  const audioCompletionRatio = lookupModelRatio(
    settings.AudioCompletionRatio,
    modelName
  )

  if (price !== undefined && price !== null) {
    return {
      ...EMPTY_PRICING_CONFIG,
      mode: 'per-request',
      fields: { ...EMPTY_PRICING_FIELDS, price: price.toString() },
    }
  }

  let promptPrice = ''
  let completionPrice = ''
  if (ratio !== undefined && ratio !== null) {
    const tokenPrice = ratio * 2
    promptPrice = tokenPrice.toString()
    if (completionRatio !== undefined && completionRatio !== null) {
      completionPrice = (tokenPrice * completionRatio).toString()
    }
  }

  return {
    mode: 'per-token',
    fields: {
      price: '',
      ratio: ratio?.toString() || '',
      cacheRatio: cacheRatio?.toString() || '',
      completionRatio: completionRatio?.toString() || '',
      imageRatio: imageRatio?.toString() || '',
      audioRatio: audioRatio?.toString() || '',
      audioCompletionRatio: audioCompletionRatio?.toString() || '',
    },
    promptPrice,
    completionPrice,
    advancedOpen: [
      cacheRatio,
      imageRatio,
      audioRatio,
      audioCompletionRatio,
    ].some((value) => value !== undefined && value !== null),
  }
}

export function shouldReplacePricingConfig(
  finalModelName: string,
  loadedPricingName: string,
  hasPricingConfig: boolean
): boolean {
  return hasPricingConfig || finalModelName === loadedPricingName
}
