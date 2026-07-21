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
import { useForm } from 'react-hook-form'
import i18n from 'i18next'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { Form } from '@/components/ui/form'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  type ChannelFormValues,
} from '../../../lib'
import { ChannelModelsSection } from './channel-models-section'

i18n.init({
  lng: 'en',
  fallbackLng: 'en',
  resources: { en: { translation: {} } },
  interpolation: { escapeValue: false },
})

const selectedModels = Array.from(
  { length: 8 },
  (_, index) => `long-vendor-model-${index + 1}`
)

function ChannelModelsSectionFixture() {
  const form = useForm<ChannelFormValues>({
    defaultValues: {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      models: selectedModels.join(','),
      model_mapping: JSON.stringify({
        'public-model': selectedModels[0],
      }),
    },
  })

  return (
    <Form {...form}>
      <ChannelModelsSection
        allModels={selectedModels}
        basicModels={selectedModels.slice(0, 2)}
        groupOptions={[{ value: 'default', label: 'default' }]}
        isLoadingGroups={false}
        isSubmitting={false}
        onFetchModels={() => undefined}
        prefillGroups={[]}
      />
    </Form>
  )
}

describe('ChannelModelsSection', () => {
  test('caps selected chips while preserving the complete model count', () => {
    const markup = renderToStaticMarkup(
      <I18nextProvider i18n={i18n}>
        <ChannelModelsSectionFixture />
      </I18nextProvider>
    )

    assert.match(markup, /Selected 8/)
    assert.match(markup, /\+2 more/)
    assert.doesNotMatch(markup, />long-vendor-model-7</)
    assert.doesNotMatch(markup, />long-vendor-model-8</)
  })

  test('keeps long mapping warnings wrap-safe and form descriptions linked', () => {
    const markup = renderToStaticMarkup(
      <I18nextProvider i18n={i18n}>
        <ChannelModelsSectionFixture />
      </I18nextProvider>
    )

    assert.match(markup, /\[overflow-wrap:anywhere\]/)
    assert.match(markup, /aria-describedby="[^"]+-form-item-description"/)
  })
})
