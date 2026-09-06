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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createReactTestEnvironment } from '@/test/react'
import { within } from '@testing-library/react'
import assert from 'node:assert/strict'
import { after, before, describe, test } from 'node:test'
import { TaskRateCardSettings } from './task-rate-card-settings'

const testEnv = createReactTestEnvironment()

before(() => testEnv.setup())

after(() => testEnv.teardown())

describe('TaskRateCardSettings billing examples', () => {
  test('keeps the MiniMax structured example preview-only', async () => {
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
      assert.equal(useButton.disabled, true)
      minimaxExample.getByText(
        'Preview only: structured MiniMax billing is not yet used for task admission or settlement. Configure a normal model price separately; requests without one are rejected.'
      )

      const exampleJson = minimaxExample.getByRole(
        'textbox'
      ) as HTMLTextAreaElement
      assert.match(exampleJson.value, /"billing_type": "minimax"/)

      const copyButton = minimaxExample.getByRole('button', {
        name: 'Copy to clipboard',
      }) as HTMLButtonElement
      assert.equal(copyButton.disabled, false)

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
