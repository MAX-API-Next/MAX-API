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
import { useTranslation } from 'react-i18next'
import { RichContent } from '@/components/rich-content'
import { useAuthStore } from '@/stores/auth-store'
import {
  getRenderableContentKind,
  getSafeIframeEmbedSrc,
} from '@/lib/renderable-content'
import { PublicLayout } from '@/components/layout'
import { Footer } from '@/components/layout/components/footer'
import { CTA, Features, Hero, HowItWorks, Stats } from './components'
import { useHomePageContent } from './hooks'

export function Home() {
  const { t } = useTranslation()
  const { auth } = useAuthStore()
  const isAuthenticated = !!auth.user
  const { content, isLoaded } = useHomePageContent()
  const customContent = content.trim()
  const contentKind = getRenderableContentKind(customContent)
  const iframeEmbedSrc = getSafeIframeEmbedSrc(customContent)

  if (!isLoaded) {
    return (
      <PublicLayout showMainContainer={false}>
        <main className='flex min-h-screen items-center justify-center'>
          <div className='text-muted-foreground'>{t('Loading...')}</div>
        </main>
      </PublicLayout>
    )
  }

  if (customContent) {
    return (
      <PublicLayout showMainContainer={false}>
        <main className='overflow-x-hidden'>
          {iframeEmbedSrc ? (
            <iframe
              src={iframeEmbedSrc}
              className='h-[calc(100vh-3.5rem)] w-full border-none'
              title={t('Custom Home Page')}
              sandbox='allow-forms allow-popups allow-scripts'
            />
          ) : (
            <div className='container mx-auto py-8'>
              <RichContent
                mode={contentKind === 'html' ? 'html' : 'markdown'}
                content={customContent}
                className='custom-home-content'
              />
            </div>
          )}
        </main>
      </PublicLayout>
    )
  }

  return (
    <PublicLayout showMainContainer={false}>
      <main className='home-page'>
        <Hero isAuthenticated={isAuthenticated} />
        <Stats />
        <Features />
        <HowItWorks />
        <CTA isAuthenticated={isAuthenticated} />
      </main>
      <Footer className='home-footer' />
    </PublicLayout>
  )
}
