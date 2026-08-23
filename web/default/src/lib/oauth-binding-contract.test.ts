/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  adminCustomOAuthUnbindPath,
  indexCustomOAuthBindings,
  selfCustomOAuthUnbindPath,
  type CustomOAuthBinding,
} from './oauth'

describe('custom OAuth binding contract', () => {
  test('indexes numeric provider IDs and exposes provider_user_id', () => {
    const binding: CustomOAuthBinding = {
      provider_id: 7,
      provider_name: 'Company SSO',
      provider_slug: 'company-sso',
      provider_icon: '',
      provider_user_id: 'external-42',
    }
    const indexed = indexCustomOAuthBindings([binding])
    assert.equal(indexed.get(7)?.provider_user_id, 'external-42')
    assert.equal(indexed.get(Number('7'))?.provider_name, 'Company SSO')
  })

  test('builds user and admin unbind endpoints from numeric IDs', () => {
    assert.equal(selfCustomOAuthUnbindPath(7), '/api/user/oauth/bindings/7')
    assert.equal(
      adminCustomOAuthUnbindPath(42, 7),
      '/api/user/42/oauth/bindings/7'
    )
  })
})
