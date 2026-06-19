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
    description: '用于 OpenAI 兼容的聊天与补全接口请求。',
  },
  {
    id: 'responses',
    label: 'responses',
    description: '用于 OpenAI Responses 风格接口请求。',
  },
  {
    id: 'claude_messages',
    label: 'Claude Messages',
    description: '用于 Anthropic Messages 兼容接口请求。',
  },
  {
    id: 'gemini_native',
    label: 'Gemini native',
    description: '用于 Gemini 原生 /v1beta 模型路由。',
  },
  {
    id: 'embeddings',
    label: 'embeddings',
    description: '用于 embedding 向量接口请求。',
  },
  {
    id: 'images',
    label: 'images',
    description: '用于图像生成或图像编辑接口请求。',
  },
  {
    id: 'audio',
    label: 'audio',
    description: '用于语音、转写、翻译或音乐任务接口。',
  },
  {
    id: 'rerank',
    label: 'rerank',
    description: '用于搜索重排序接口请求。',
  },
  {
    id: 'video_tasks',
    label: 'video tasks',
    description: '用于异步视频生成任务接口。',
  },
  {
    id: 'model_discovery',
    label: 'model discovery',
    description: '用于直接从上游拉取模型列表。',
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
  native: '原生支持',
  compatible: '兼容支持',
  configurable: '可配置',
  limited: '有限支持',
  unsupported: '不支持',
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
