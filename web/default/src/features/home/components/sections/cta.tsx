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
import { Link } from '@tanstack/react-router'
import { ArrowRight, BookOpen, GitFork, ServerCog } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useStatus } from '@/hooks/use-status'
import { Button } from '@/components/ui/button'
import { AnimateInView } from '@/components/animate-in-view'

interface CTAProps {
  className?: string
  isAuthenticated?: boolean
}

const COMMUNITY_POINTS = [
  'Developers integrating multiple model providers',
  'Research groups and university teams building AI systems',
  'Organizations operating Agents, workflows, and internal AI platforms',
  'Teams that need cost audit, private deployment, and governance boundaries',
] as const

export function CTA(props: CTAProps) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const docsUrl =
    (status?.docs_link as string | undefined) ||
    'https://github.com/MAX-API-Next/MAX-API'

  if (props.isAuthenticated) {
    return null
  }

  const docsButton = docsUrl.startsWith('http') ? (
    <Button
      variant='outline'
      size='lg'
      className='home-hero-button home-hero-button--ghost'
      render={<a href={docsUrl} target='_blank' rel='noopener noreferrer' />}
    >
      <BookOpen data-icon='inline-start' />
      {t('Docs')}
    </Button>
  ) : (
    <Button
      variant='outline'
      size='lg'
      className='home-hero-button home-hero-button--ghost'
      render={<Link to={docsUrl} />}
    >
      <BookOpen data-icon='inline-start' />
      {t('Docs')}
    </Button>
  )

  return (
    <section className='home-section px-4 py-16 sm:px-6 md:py-24'>
      <AnimateInView className='home-cta-panel' animation='scale-in'>
        <div className='home-cta-orbit' aria-hidden='true' />
        <div className='home-section-kicker'>
          <ServerCog aria-hidden='true' />
          {t('Open-source AI governance community')}
        </div>
        <div className='home-cta-content'>
          <div>
            <h2>{t('Build with the MAX API Next community')}</h2>
            <p>
              {t(
                'The community focuses on model platform adaptation, AgentOps engineering practice, cost audit, governance boundaries, multimodal task protocols, and infrastructure for AI applications that need to keep running after the demo.'
              )}
            </p>
          </div>
          <div className='home-cta-actions'>
            <Button
              size='lg'
              className='home-hero-button home-hero-button--primary'
              render={<Link to='/sign-up' />}
            >
              {t('Get Started')}
              <ArrowRight data-icon='inline-end' />
            </Button>
            {docsButton}
          </div>
        </div>
        <div className='home-community-grid'>
          {COMMUNITY_POINTS.map((point) => (
            <div key={point} className='home-community-point'>
              <GitFork aria-hidden='true' />
              <span>{t(point)}</span>
            </div>
          ))}
        </div>
      </AnimateInView>
    </section>
  )
}
