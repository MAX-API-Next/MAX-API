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
import { act, useState } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createReactTestEnvironment } from '@/test/react'
import { within } from '@testing-library/react'
import assert from 'node:assert/strict'
import { after, test } from 'node:test'

const env = createReactTestEnvironment()
await env.setup()
const { ModelsProvider } = await import('../models-provider')
const { SyncWizardDialog } = await import('./sync-wizard-dialog')
after(() => env.teardown())

test('sync language draft survives unrelated renders and resets on reopen', async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  function Harness() {
    const [open, setOpen] = useState(true)
    const [, setRevision] = useState(0)
    return (
      <>
        <button onClick={() => setRevision((value) => value + 1)}>
          Rerender host
        </button>
        <button onClick={() => setOpen((value) => !value)}>
          Toggle wizard
        </button>
        <SyncWizardDialog open={open} onOpenChange={setOpen} />
      </>
    )
  }
  const view = await env.render(
    <QueryClientProvider client={client}>
      <ModelsProvider>
        <Harness />
      </ModelsProvider>
    </QueryClientProvider>
  )
  const body = within(env.dom.window.document.body)
  const selected = (locale: string) =>
    body
      .getByRole('radio', { name: locale === 'en' ? 'English' : 'Chinese' })
      .getAttribute('aria-checked')
  try {
    await view.click(body.getByRole('radio', { name: 'English' }))
    assert.equal(selected('en'), 'true')
    await view.click(within(view.container).getByText('Rerender host'))
    await act(async () => {
      await env.i18n.changeLanguage('fr')
    })
    assert.equal(selected('en'), 'true')
    await view.click(within(view.container).getByText('Toggle wizard'))
    await view.click(within(view.container).getByText('Toggle wizard'))
    assert.equal(selected('zh'), 'true')
  } finally {
    await view.unmount()
    client.clear()
  }
})
