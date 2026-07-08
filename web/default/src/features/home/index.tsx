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
import { useCallback, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import {
  getRenderableContentKind,
  getSafeIframeEmbedSrc,
} from '@/lib/renderable-content'
import { useTheme } from '@/context/theme-provider'
import { PublicLayout } from '@/components/layout'
import { Footer } from '@/components/layout/components/footer'
import { RichContent } from '@/components/rich-content'
import { CTA, Features, Hero, HowItWorks, Stats } from './components'
import { useHomePageContent } from './hooks'

export function Home() {
  const { i18n, t } = useTranslation()
  const iframeRef = useRef<HTMLIFrameElement>(null)
  const { resolvedTheme } = useTheme()
  const { auth } = useAuthStore()
  const isAuthenticated = !!auth.user
  const { content, isLoaded } = useHomePageContent()
  const customContent = content.trim()
  const contentKind = getRenderableContentKind(customContent)
  const iframeEmbedSrc = getSafeIframeEmbedSrc(customContent)

  const syncIframePreferences = useCallback(() => {
    try {
      iframeRef.current?.contentWindow?.postMessage(
        { themeMode: resolvedTheme },
        '*'
      )
      iframeRef.current?.contentWindow?.postMessage(
        { lang: i18n.language },
        '*'
      )
    } catch {
      // Cross-origin frames can reject access while navigating.
    }
  }, [i18n.language, resolvedTheme])

  useEffect(() => {
    if (iframeEmbedSrc) {
      syncIframePreferences()
    }
  }, [iframeEmbedSrc, syncIframePreferences])

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
              ref={iframeRef}
              src={iframeEmbedSrc}
              className='h-[calc(100vh-3.5rem)] w-full border-none'
              title={t('Custom Home Page')}
              sandbox='allow-forms allow-popups allow-popups-to-escape-sandbox allow-scripts'
              onLoad={syncIframePreferences}
            />
          ) : (
            <div className='container mx-auto py-8'>
              <RichContent
                mode={contentKind === 'html' ? 'html' : 'markdown'}
                content={customContent}
                className='custom-home-content'
                htmlVariant={contentKind === 'html' ? 'isolated' : undefined}
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
