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
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { handleServerError } from '@/lib/handle-server-error'
import { getUserModels, getUserGroups } from './api'
import { PlaygroundChat } from './components/playground-chat'
import { PlaygroundInput } from './components/playground-input'
import { usePlaygroundState, useChatHandler } from './hooks'
import { createUserMessage, createLoadingAssistantMessage } from './lib'
import { queryDataOrEmptyOnError } from './lib/query-state'
import {
  getAvailableOptionValue,
  isOptionValueAvailable,
} from './lib/selection-state'
import type { Message as MessageType } from './types'

export function Playground() {
  const { t } = useTranslation()
  const {
    config,
    parameterEnabled,
    messages,
    models,
    groups,
    updateMessages,
    setModels,
    setGroups,
    updateConfig,
    clearMessages,
  } = usePlaygroundState()

  const { sendChat, stopGeneration, isGenerating } = useChatHandler({
    config,
    parameterEnabled,
    onMessageUpdate: updateMessages,
  })

  // Edit dialog state
  const [editingMessageKey, setEditingMessageKey] = useState<string | null>(
    null
  )

  // Load models
  const {
    data: modelsData,
    error: modelsError,
    isLoading: isLoadingModels,
  } = useQuery({
    queryKey: ['playground-models'],
    queryFn: getUserModels,
  })

  // Load groups
  const { data: groupsData, error: groupsError } = useQuery({
    queryKey: ['playground-groups'],
    queryFn: getUserGroups,
  })
  const effectiveModelsData = useMemo(
    () => queryDataOrEmptyOnError(modelsData, modelsError),
    [modelsData, modelsError]
  )
  const effectiveGroupsData = useMemo(
    () => queryDataOrEmptyOnError(groupsData, groupsError),
    [groupsData, groupsError]
  )
  const canSendChat = useMemo(() => {
    return (
      isOptionValueAvailable(effectiveModelsData, config.model) &&
      isOptionValueAvailable(effectiveGroupsData, config.group)
    )
  }, [effectiveModelsData, effectiveGroupsData, config.model, config.group])

  useEffect(() => {
    if (!modelsError) return

    handleServerError(modelsError)
  }, [modelsError])

  useEffect(() => {
    if (!groupsError) return

    handleServerError(groupsError)
  }, [groupsError])

  // Update models when data changes
  useEffect(() => {
    if (!effectiveModelsData) return

    setModels(effectiveModelsData)

    const nextModel = getAvailableOptionValue(
      effectiveModelsData,
      config.model
    )
    if (nextModel !== config.model) {
      updateConfig('model', nextModel)
    }
  }, [effectiveModelsData, config.model, setModels, updateConfig])

  // Update groups when data changes
  useEffect(() => {
    if (!effectiveGroupsData) return

    setGroups(effectiveGroupsData)

    const nextGroup = getAvailableOptionValue(
      effectiveGroupsData,
      config.group,
      'default'
    )
    if (nextGroup !== config.group) {
      updateConfig('group', nextGroup)
    }
  }, [effectiveGroupsData, setGroups, config.group, updateConfig])

  const handleSendMessage = (text: string) => {
    if (!canSendChat) return

    const userMessage = createUserMessage(text)
    const assistantMessage = createLoadingAssistantMessage()

    const newMessages = [...messages, userMessage, assistantMessage]
    updateMessages(newMessages)

    // Send chat request
    sendChat(newMessages)
  }

  const handleCopyMessage = (message: MessageType) => {
    // Copy is handled in MessageActions component
    // eslint-disable-next-line no-console
    console.log('Message copied:', message.key)
  }

  const handleRegenerateMessage = (message: MessageType) => {
    if (!canSendChat) return

    // Find the message index and regenerate from there
    const messageIndex = messages.findIndex((m) => m.key === message.key)
    if (messageIndex === -1) return

    // Remove messages after this one and regenerate
    const messagesUpToHere = messages.slice(0, messageIndex)
    const loadingMessage = createLoadingAssistantMessage()
    const newMessages = [...messagesUpToHere, loadingMessage]

    updateMessages(newMessages)
    sendChat(newMessages)
  }

  const handleEditMessage = useCallback((message: MessageType) => {
    setEditingMessageKey(message.key)
  }, [])

  const handleEditOpenChange = useCallback((open: boolean) => {
    if (!open) setEditingMessageKey(null)
  }, [])

  // Apply edit and optionally re-submit from the edited user message
  const applyEdit = useCallback(
    (newContent: string, submit: boolean) => {
      if (!editingMessageKey) return
      const index = messages.findIndex((m) => m.key === editingMessageKey)
      if (index === -1) return

      const updated = messages.map((m) =>
        m.key === editingMessageKey
          ? { ...m, versions: [{ ...m.versions[0], content: newContent }] }
          : m
      )

      setEditingMessageKey(null)

      if (!submit || updated[index].from !== 'user') {
        updateMessages(updated)
        return
      }

      if (!canSendChat) {
        updateMessages(updated)
        return
      }

      const toSubmit = [
        ...updated.slice(0, index + 1),
        createLoadingAssistantMessage(),
      ]
      updateMessages(toSubmit)
      sendChat(toSubmit)
    },
    [editingMessageKey, messages, updateMessages, sendChat, canSendChat]
  )

  const handleDeleteMessage = (message: MessageType) => {
    const newMessages = messages.filter((m) => m.key !== message.key)
    updateMessages(newMessages)
  }

  const handleClearHistory = useCallback(() => {
    if (isGenerating || messages.length === 0) return

    clearMessages()
    setEditingMessageKey(null)
    toast.success(t('Chat history cleared'))
  }, [clearMessages, isGenerating, messages.length, t])

  return (
    <div className='relative flex size-full flex-col overflow-hidden'>
      {/* Full-width scroll container: scrolling works even over side whitespace */}
      <div className='flex flex-1 flex-col overflow-hidden'>
        <PlaygroundChat
          messages={messages}
          onCopyMessage={handleCopyMessage}
          onRegenerateMessage={handleRegenerateMessage}
          onEditMessage={handleEditMessage}
          onDeleteMessage={handleDeleteMessage}
          isGenerating={isGenerating}
          editingKey={editingMessageKey}
          onCancelEdit={handleEditOpenChange}
          onSaveEdit={(newContent) => applyEdit(newContent, false)}
          onSaveEditAndSubmit={(newContent) => applyEdit(newContent, true)}
        />
      </div>

      {/* Input area: center content and constrain to the same container width */}
      <div className='mx-auto w-full max-w-4xl'>
        <PlaygroundInput
          disabled={isGenerating}
          groups={groups}
          groupValue={config.group}
          isGenerating={isGenerating}
          isModelLoading={isLoadingModels}
          isSubmitDisabled={!canSendChat}
          modelValue={config.model}
          models={models}
          hasMessages={messages.length > 0}
          onGroupChange={(value) => updateConfig('group', value)}
          onClearHistory={handleClearHistory}
          onModelChange={(value) => updateConfig('model', value)}
          onStop={stopGeneration}
          onSubmit={handleSendMessage}
        />
      </div>
    </div>
  )
}
