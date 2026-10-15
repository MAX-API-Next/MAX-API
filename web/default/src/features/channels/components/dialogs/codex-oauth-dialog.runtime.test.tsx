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
import { createReactTestEnvironment } from '@/test/react'
import assert from 'node:assert/strict'
import { after, test } from 'node:test'
import { api } from '@/lib/api'

const env = createReactTestEnvironment()
await env.setup()
const { fireEvent, within } = await import('@testing-library/react/pure')
const { CodexOAuthDialog } = await import('./codex-oauth-dialog')
after(() => env.teardown())

for (const transition of ['editor', 'reopen'] as const)
  test('late OAuth completion is ignored after ' + transition, async () => {
    const adapter = api.defaults.adapter
    let release!: () => void, started!: () => void
    const ready = new Promise<void>((resolve) => {
      started = resolve
    })
    const assigned: string[] = []
    const requests: string[] = []
    api.defaults.adapter = async (config) => {
      requests.push(config.url || '')
      assert.equal(config.url, '/api/channel/codex/oauth/complete')
      started()
      await new Promise<void>((resolve) => {
        release = resolve
      })
      return {
        config,
        status: 200,
        statusText: 'OK',
        headers: {},
        data: {
          success: true,
          data: { key: 'synthetic-credential-for-editor-one' },
        },
      }
    }
    function Harness() {
      const [editor, setEditor] = useState('one')
      const [open, setOpen] = useState(true)
      return (
        <>
          <button
            onClick={() =>
              transition === 'editor'
                ? setEditor('two')
                : setOpen((value) => !value)
            }
          >
            Change editor
          </button>
          <CodexOAuthDialog
            open={open}
            scopeKey={editor}
            onOpenChange={() => {}}
            onKeyGenerated={(key) => assigned.push(key)}
          />
        </>
      )
    }
    const view = await env.render(<Harness />)
    const body = within(env.dom.window.document.body)
    try {
      const input = body.getByPlaceholderText(
        'Paste the full callback URL (includes code & state)'
      ) as HTMLInputElement
      await act(async () => {
        fireEvent.change(input, {
          target: {
            value: 'http://localhost/callback?code=synthetic&state=synthetic',
          },
        })
      })
      const generate = body.getByRole('button', {
        name: 'Generate credential',
      }) as HTMLButtonElement
      assert.equal(generate.disabled, false)
      await view.click(generate)
      await ready
      await view.click(within(view.container).getByText('Change editor'))
      if (transition === 'reopen')
        await view.click(within(view.container).getByText('Change editor'))
      await act(async () => release())
      assert.deepEqual(assigned, [])
      assert.equal(
        (
          body.getByPlaceholderText(
            'Paste the full callback URL (includes code & state)'
          ) as HTMLInputElement
        ).value,
        ''
      )
      assert.deepEqual(requests, ['/api/channel/codex/oauth/complete'])
    } finally {
      release?.()
      await view.unmount()
      api.defaults.adapter = adapter
    }
  })
