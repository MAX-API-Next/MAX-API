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
import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createReactTestEnvironment } from '@/test/react'
import { fireEvent, within } from '@testing-library/react'
import assert from 'node:assert/strict'
import { after, before, describe, test } from 'node:test'
import { SettingsPageProvider } from '../components/settings-page-context'
import { TaskRateCardSettings } from './task-rate-card-settings'
import { TieredBillingSettings } from './tiered-billing-settings'

const testEnv = createReactTestEnvironment()

before(() => testEnv.setup())

after(() => testEnv.teardown())

for (const kind of ['task', 'tiered'] as const) {
  test(`${kind} settings retain valid editor state after failed file imports`, async () => {
    const queryClient = new QueryClient()
    const actions = document.createElement('div')
    document.body.append(actions)
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <SettingsPageProvider actionsContainer={actions}>
          {kind === 'task' ? (
            <TaskRateCardSettings defaultValue='{}' />
          ) : (
            <TieredBillingSettings billingMode='{}' billingExpr='{}' />
          )}
        </SettingsPageProvider>
      </QueryClientProvider>
    )
    try {
      const editor = [...view.container.querySelectorAll('textarea')].find(
        (input) => !input.readOnly
      )
      assert.ok(editor)
      const save = within(actions).getByRole('button', {
        name: kind === 'task' ? 'Save task rate cards' : 'Save tiered billing',
      }) as HTMLButtonElement
      const fileInput = (
        kind === 'task' ? view.container : actions
      ).querySelector('input[type=file]')
      assert.ok(fileInput)
      const original = editor.value
      assert.equal(save.disabled, false)
      for (const text of [
        async () => '{',
        async () => '[]',
        async () => {
          throw new Error('Synthetic file read failure')
        },
      ]) {
        await act(async () => {
          fireEvent.change(fileInput, { target: { files: [{ text }] } })
        })
        assert.equal(editor.value, original)
        assert.equal(save.disabled, false)
      }
      const imported =
        kind === 'task'
          ? '{"test-model":{"unit":"second","rows":[]}}'
          : '{"test-model":{"enabled":true,"expr":"p * 1"}}'
      await act(async () => {
        fireEvent.change(fileInput, {
          target: { files: [{ text: async () => imported }] },
        })
      })
      assert.deepEqual(JSON.parse(editor.value), JSON.parse(imported))
      assert.equal(save.disabled, false)
    } finally {
      await view.unmount()
      actions.remove()
      queryClient.clear()
    }
  })
}

describe('TaskRateCardSettings billing examples', () => {
  test('allows the active MiniMax structured example to be loaded', async () => {
    const queryClient = new QueryClient()
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <TaskRateCardSettings defaultValue='{}' />
      </QueryClientProvider>
    )

    try {
      const screen = within(view.container)
      const minimaxHeading = screen.getByRole('heading', {
        name: 'MiniMax billing example',
      })
      const minimaxSection = minimaxHeading.closest('section')
      assert.ok(minimaxSection)

      const minimaxExample = within(minimaxSection)
      const useButton = minimaxExample.getByRole('button', {
        name: 'Use example',
      }) as HTMLButtonElement
      assert.equal(useButton.disabled, false)
      assert.equal(minimaxExample.queryByRole('alert'), null)

      const exampleJson = minimaxExample.getByRole(
        'textbox'
      ) as HTMLTextAreaElement
      assert.match(exampleJson.value, /"billing_type": "minimax"/)

      const copyButton = minimaxExample.getByRole('button', {
        name: 'Copy to clipboard',
      }) as HTMLButtonElement
      assert.equal(copyButton.disabled, false)

      await view.click(useButton)
      const currentEditor = screen.getByRole('textbox', {
        name: 'Current rate card JSON',
      }) as HTMLTextAreaElement
      assert.match(currentEditor.value, /"billing_type": "minimax"/)

      const klingHeading = screen.getByRole('heading', {
        name: 'Kling billing example',
      })
      const klingSection = klingHeading.closest('section')
      assert.ok(klingSection)
      const klingUseButton = within(klingSection).getByRole('button', {
        name: 'Use example',
      }) as HTMLButtonElement
      assert.equal(klingUseButton.disabled, false)
    } finally {
      await view.unmount()
      queryClient.clear()
    }
  })

  test('does not re-render billing examples when rate-card text changes', async () => {
    const queryClient = new QueryClient()
    const originalTranslate = testEnv.i18n.t
    const titleCalls = {
      kling: 0,
      minimax: 0,
    }

    testEnv.i18n.t = new Proxy(originalTranslate, {
      apply(target, thisArg, args) {
        if (args[0] === 'Kling billing example') titleCalls.kling += 1
        if (args[0] === 'MiniMax billing example') titleCalls.minimax += 1
        return Reflect.apply(target, thisArg, args)
      },
    })

    try {
      const view = await testEnv.render(
        <QueryClientProvider client={queryClient}>
          <TaskRateCardSettings defaultValue='{}' />
        </QueryClientProvider>
      )

      try {
        assert.ok(titleCalls.kling > 0)
        assert.ok(titleCalls.minimax > 0)
        titleCalls.kling = 0
        titleCalls.minimax = 0

        const screen = within(view.container)
        const klingHeading = screen.getByRole('heading', {
          name: 'Kling billing example',
        })
        const klingSection = klingHeading.closest('section')
        assert.ok(klingSection)
        const klingUseButton = within(klingSection).getByRole('button', {
          name: 'Use example',
        }) as HTMLButtonElement
        await view.click(klingUseButton)

        const currentEditor = screen.getByRole('textbox', {
          name: 'Current rate card JSON',
        }) as HTMLTextAreaElement
        assert.match(currentEditor.value, /kling\/kling-v3-video-generation/)
        screen.getByText('kling/kling-v3-video-generation')
        assert.equal(titleCalls.kling, 0)
        assert.equal(titleCalls.minimax, 0)
      } finally {
        await view.unmount()
      }
    } finally {
      testEnv.i18n.t = originalTranslate
      queryClient.clear()
    }
  })
})
