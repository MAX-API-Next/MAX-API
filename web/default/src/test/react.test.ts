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
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createReactTestEnvironment } from './react'

test('teardown cancels pending animation frames before removing browser globals', async () => {
  const testEnv = createReactTestEnvironment()
  await testEnv.setup()
  let callbackRan = false

  requestAnimationFrame(() => {
    callbackRan = true
    void window.document
  })

  testEnv.teardown()
  await new Promise((resolve) => setTimeout(resolve, 10))

  assert.equal(callbackRan, false)
})

test('a captured animation frame scheduler cannot leak across test environments', async () => {
  const firstEnv = createReactTestEnvironment()
  await firstEnv.setup()
  const capturedRequestAnimationFrame = requestAnimationFrame
  firstEnv.teardown()

  const secondEnv = createReactTestEnvironment()
  await secondEnv.setup()
  let callbackRan = false

  capturedRequestAnimationFrame(() => {
    callbackRan = true
    void window.document
  })

  secondEnv.teardown()
  await new Promise((resolve) => setTimeout(resolve, 10))

  assert.equal(callbackRan, false)
})
