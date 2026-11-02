/*
Copyright (C) 2023-2026 MAX-API-Next
Copyright (C) 2023-2026 QuantumNous

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
import { useEffect } from 'react'
import { useNavigate, useRouter } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'

const FEEDBACK_URL = 'https://github.com/MAX-API-Next/MAX-API/issues'
const CHUNK_RELOAD_KEY = 'maxapi:chunk-reload-build'

type GeneralErrorProps = React.HTMLAttributes<HTMLDivElement> & {
  minimal?: boolean
  error?: unknown
}

function getHttpStatus(error: unknown): number | undefined {
  if (typeof error !== 'object' || error === null) return undefined
  const response = (error as Record<string, unknown>).response
  if (typeof response !== 'object' || response === null) return undefined
  const status = (response as Record<string, unknown>).status
  return typeof status === 'number' ? status : undefined
}

export function GeneralError({
  className,
  minimal = false,
  error,
}: GeneralErrorProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { history } = useRouter()
  const status = getHttpStatus(error)
  const isChunkError =
    status === undefined &&
    error instanceof Error &&
    (error.name === 'ChunkLoadError' ||
      /^Loading (CSS )?chunk .+ failed\./.test(error.message) ||
      ('code' in error && error.code === 'CSS_CHUNK_LOAD_FAILED'))

  // Adapted from QuantumNous/new-api b892a6b2e (AGPL-3.0).
  // Run only when a failed route is displayed, never during intent preload.
  useEffect(() => {
    if (!isChunkError || !(error instanceof Error)) return
    if (window.navigator.onLine === false) return

    try {
      // Deferred entry scripts identify the loaded build, even when VERSION is
      // reused. Async chunk scripts are not deferred and must not change this key.
      const entries = Array.from(
        document.querySelectorAll<HTMLScriptElement>('script[defer][src]')
      ).map((script) => script.src)
      const build = entries.length ? entries.join('\n') : error.message
      if (window.sessionStorage.getItem(CHUNK_RELOAD_KEY) === build) return
      window.sessionStorage.setItem(CHUNK_RELOAD_KEY, build)
    } catch {
      // A durable marker is required to avoid an automatic reload loop.
      return
    }
    window.location.reload()
  }, [error, isChunkError])

  const isRateLimited = status === 429
  let title = `${t('Oops! Something went wrong')} ${`:')`}`
  let description = t('Please try again later.')
  if (isChunkError) {
    title = t('Page resources could not be loaded')
    description = t(
      'The application may have been updated. Reload the page to continue.'
    )
  } else if (isRateLimited) {
    title = t('Too many requests')
    description = t('Please wait a moment before trying again.')
  }

  return (
    <div className={cn('h-svh w-full', className)}>
      <div className='m-auto flex h-full w-full flex-col items-center justify-center gap-2'>
        {!minimal && status !== undefined && (
          <h1 className='text-[7rem] leading-tight font-bold'>{status}</h1>
        )}
        <span className='font-medium'>{title}</span>
        <p className='text-muted-foreground text-center'>
          {t('We apologize for the inconvenience.')} <br /> {description}
        </p>
        {!minimal && (
          <p className='text-muted-foreground text-center text-sm'>
            {t('If this keeps happening, please report it on GitHub Issues.')}
          </p>
        )}
        {!minimal && (
          <div className='mt-6 flex flex-wrap justify-center gap-4'>
            {isChunkError && (
              <Button onClick={() => window.location.reload()}>
                {t('Reload page')}
              </Button>
            )}
            <Button variant='outline' onClick={() => history.go(-1)}>
              {t('Go Back')}
            </Button>
            <Button
              variant='outline'
              render={
                <a
                  href={FEEDBACK_URL}
                  target='_blank'
                  rel='noopener noreferrer'
                />
              }
            >
              {t('Report an issue')}
            </Button>
            <Button
              variant={isChunkError ? 'outline' : 'default'}
              onClick={() => navigate({ to: '/' })}
            >
              {t('Back to Home')}
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}
