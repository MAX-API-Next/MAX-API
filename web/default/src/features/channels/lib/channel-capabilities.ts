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
import { CHANNEL_TYPES, MODEL_FETCHABLE_TYPES } from '../constants'

export const VIDEO_TASK_CHANNEL_TYPES = new Set([
  1, 17, 24, 35, 41, 45, 50, 51, 52, 54, 55,
])

export const CHANNEL_CAPABILITY_DEFINITIONS = [
  {
    id: 'chat',
    label: 'chat/completions',
    description: 'Routes OpenAI-compatible chat and completion requests.',
  },
  {
    id: 'responses',
    label: 'responses',
    description: 'Routes OpenAI Responses API requests.',
  },
  {
    id: 'claude_messages',
    label: 'Claude Messages',
    description: 'Routes Anthropic Messages-compatible requests.',
  },
  {
    id: 'gemini_native',
    label: 'Gemini native',
    description: 'Routes Gemini native /v1beta model requests.',
  },
  {
    id: 'embeddings',
    label: 'embeddings',
    description: 'Routes embedding vector requests.',
  },
  {
    id: 'images',
    label: 'images',
    description: 'Routes image generation and editing requests.',
  },
  {
    id: 'audio',
    label: 'audio',
    description:
      'Routes speech, transcription, translation, or music requests.',
  },
  {
    id: 'rerank',
    label: 'rerank',
    description: 'Routes search reranking requests.',
  },
  {
    id: 'video_tasks',
    label: 'video tasks',
    description: 'Routes asynchronous video generation tasks.',
  },
  {
    id: 'model_discovery',
    label: 'model discovery',
    description: 'Fetches model lists directly from the upstream provider.',
  },
] as const

export type ChannelCapabilityId =
  (typeof CHANNEL_CAPABILITY_DEFINITIONS)[number]['id']

export type ChannelCapabilityStatus =
  | 'native'
  | 'compatible'
  | 'configurable'
  | 'limited'
  | 'unsupported'

export const CHANNEL_CAPABILITY_STATUS_LABELS: Record<
  ChannelCapabilityStatus,
  string
> = {
  native: 'Native support',
  compatible: 'Compatible support',
  configurable: 'Configurable',
  limited: 'Limited support',
  unsupported: 'Not supported',
}

export const CHANNEL_CAPABILITY_STATUS_VARIANTS: Record<
  ChannelCapabilityStatus,
  'success' | 'info' | 'warning' | 'neutral'
> = {
  native: 'success',
  compatible: 'info',
  configurable: 'warning',
  limited: 'neutral',
  unsupported: 'neutral',
}

const DEFAULT_CAPABILITIES: Record<
  ChannelCapabilityId,
  ChannelCapabilityStatus
> = {
  chat: 'compatible',
  responses: 'limited',
  claude_messages: 'unsupported',
  gemini_native: 'unsupported',
  embeddings: 'limited',
  images: 'limited',
  audio: 'limited',
  rerank: 'unsupported',
  video_tasks: 'unsupported',
  model_discovery: 'unsupported',
}

const UNSUPPORTED_CAPABILITIES: Record<
  ChannelCapabilityId,
  ChannelCapabilityStatus
> = {
  chat: 'unsupported',
  responses: 'unsupported',
  claude_messages: 'unsupported',
  gemini_native: 'unsupported',
  embeddings: 'unsupported',
  images: 'unsupported',
  audio: 'unsupported',
  rerank: 'unsupported',
  video_tasks: 'unsupported',
  model_discovery: 'unsupported',
}

const CHANNEL_CAPABILITY_OVERRIDES: Record<
  number,
  Partial<Record<ChannelCapabilityId, ChannelCapabilityStatus>>
> = {
  1: {
    chat: 'native',
    responses: 'native',
    embeddings: 'native',
    images: 'native',
    audio: 'native',
    video_tasks: 'configurable',
  },
  2: {
    ...UNSUPPORTED_CAPABILITIES,
    images: 'native',
    video_tasks: 'limited',
  },
  3: {
    chat: 'native',
    responses: 'limited',
    embeddings: 'native',
    images: 'limited',
    audio: 'limited',
  },
  4: {
    chat: 'native',
    embeddings: 'native',
    images: 'unsupported',
    audio: 'unsupported',
  },
  5: {
    ...UNSUPPORTED_CAPABILITIES,
    images: 'native',
    video_tasks: 'limited',
  },
  14: {
    chat: 'compatible',
    responses: 'limited',
    claude_messages: 'native',
    embeddings: 'unsupported',
    images: 'unsupported',
    audio: 'unsupported',
  },
  17: {
    chat: 'native',
    embeddings: 'limited',
    images: 'limited',
    audio: 'limited',
    video_tasks: 'configurable',
  },
  20: {
    chat: 'compatible',
    responses: 'limited',
    claude_messages: 'limited',
    gemini_native: 'limited',
    embeddings: 'limited',
    images: 'limited',
  },
  22: {
    chat: 'limited',
    responses: 'unsupported',
    embeddings: 'unsupported',
    images: 'unsupported',
    audio: 'unsupported',
  },
  24: {
    chat: 'compatible',
    gemini_native: 'native',
    embeddings: 'native',
    images: 'native',
    video_tasks: 'configurable',
  },
  33: {
    chat: 'compatible',
    claude_messages: 'limited',
    gemini_native: 'limited',
    images: 'limited',
    audio: 'unsupported',
  },
  34: {
    chat: 'limited',
    embeddings: 'native',
    rerank: 'native',
    images: 'unsupported',
    audio: 'unsupported',
  },
  35: {
    chat: 'native',
    images: 'limited',
    audio: 'native',
    video_tasks: 'configurable',
  },
  36: {
    ...UNSUPPORTED_CAPABILITIES,
    audio: 'native',
  },
  37: {
    ...UNSUPPORTED_CAPABILITIES,
    chat: 'limited',
  },
  38: {
    ...UNSUPPORTED_CAPABILITIES,
    embeddings: 'native',
    rerank: 'native',
  },
  41: {
    chat: 'compatible',
    gemini_native: 'native',
    embeddings: 'native',
    images: 'native',
    video_tasks: 'configurable',
  },
  45: {
    chat: 'native',
    embeddings: 'limited',
    images: 'limited',
    video_tasks: 'configurable',
  },
  49: {
    ...UNSUPPORTED_CAPABILITIES,
    chat: 'limited',
  },
  50: {
    ...UNSUPPORTED_CAPABILITIES,
    video_tasks: 'native',
  },
  51: {
    ...UNSUPPORTED_CAPABILITIES,
    images: 'native',
    video_tasks: 'native',
  },
  52: {
    ...UNSUPPORTED_CAPABILITIES,
    video_tasks: 'native',
  },
  54: {
    ...UNSUPPORTED_CAPABILITIES,
    video_tasks: 'native',
  },
  55: {
    ...UNSUPPORTED_CAPABILITIES,
    video_tasks: 'native',
  },
  56: {
    chat: 'limited',
    images: 'limited',
    audio: 'limited',
    video_tasks: 'limited',
  },
  57: {
    ...UNSUPPORTED_CAPABILITIES,
    chat: 'limited',
  },
}

export type ChannelCapabilityRow = {
  id: ChannelCapabilityId
  label: string
  description: string
  status: ChannelCapabilityStatus
}

export function getChannelCapabilityRows(type: number): ChannelCapabilityRow[] {
  const typeId = Number(type)
  const hasKnownType = typeId > 0 && typeId in CHANNEL_TYPES
  const base = hasKnownType ? DEFAULT_CAPABILITIES : UNSUPPORTED_CAPABILITIES
  const capabilities = {
    ...base,
    ...CHANNEL_CAPABILITY_OVERRIDES[typeId],
  }

  if (MODEL_FETCHABLE_TYPES.has(typeId)) {
    capabilities.model_discovery = 'native'
  }
  if (
    VIDEO_TASK_CHANNEL_TYPES.has(typeId) &&
    capabilities.video_tasks === 'unsupported'
  ) {
    capabilities.video_tasks = 'configurable'
  }

  return CHANNEL_CAPABILITY_DEFINITIONS.map((definition) => ({
    ...definition,
    status: capabilities[definition.id],
  }))
}

export function isVideoTaskChannelType(type: number): boolean {
  return VIDEO_TASK_CHANNEL_TYPES.has(Number(type))
}
