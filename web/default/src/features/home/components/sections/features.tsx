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
import {
  Binary,
  CircleDollarSign,
  FileSearch,
  GitBranch,
  RadioTower,
  Workflow,
  type LucideIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { AnimateInView } from '@/components/animate-in-view'

interface FeatureCard {
  title: string
  description: string
  eyebrow: string
  icon: LucideIcon
  span?: string
  accent: string
  sample: string[]
}

export function Features() {
  const { t } = useTranslation()
  const features: FeatureCard[] = [
    {
      title: t('AI asset governance'),
      description: t(
        'Unify model catalogs, channel mappings, group access, pricing rules, and multimodal task capabilities across authorized providers.'
      ),
      eyebrow: t('Model governance'),
      icon: RadioTower,
      span: 'lg:col-span-2',
      accent: 'text-cyan-600 dark:text-cyan-300',
      sample: ['catalog', 'mapping', 'groups', 'pricing', 'DeepSeek', 'Qwen'],
    },
    {
      title: t('Intuitive cost audit'),
      description: t(
        'Make model pricing, token usage, async task costs, refunds, and Agent cost attribution visible and auditable.'
      ),
      eyebrow: t('Cost audit'),
      icon: CircleDollarSign,
      accent: 'text-amber-600 dark:text-amber-300',
      sample: ['p * 2.5', 'cache discount', '720p / 5s', 'audio flag'],
    },
    {
      title: t('AgentOps control plane'),
      description: t(
        'Issue scoped API keys for Agents and workflows, then govern model access, quotas, cost attribution, request traces, and audit visibility.'
      ),
      eyebrow: t('AgentOps'),
      icon: Workflow,
      accent: 'text-emerald-600 dark:text-emerald-300',
      sample: ['Agent keys', 'model limits', 'usage logs', 'audit'],
    },
    {
      title: t('Provider protocol governance'),
      description: t(
        'Normalize OpenAI Compatible, Responses, Claude Messages, Gemini, Realtime, and configurable non-standard video task protocols.'
      ),
      eyebrow: t('Protocol layer'),
      icon: Binary,
      span: 'lg:col-span-2',
      accent: 'text-sky-600 dark:text-sky-300',
      sample: ['/v1/chat', '/v1/responses', '/v1/messages', '/v1/videos'],
    },
    {
      title: t('Routing and reliability governance'),
      description: t(
        'Route by model, group, weight, channel state, and failure policy with retries, pre-charge, refunds, limits, and operational visibility.'
      ),
      eyebrow: t('Operations'),
      icon: GitBranch,
      span: 'lg:col-span-2',
      accent: 'text-lime-700 dark:text-lime-300',
      sample: ['weighted', 'retry', 'rate limit', 'refund'],
    },
    {
      title: t('Compliance-aware audit surface'),
      description: t(
        'Keep sensitive request and response logging behind admin-only controls, explicit retention decisions, and deployment-side compliance boundaries.'
      ),
      eyebrow: t('Security'),
      icon: FileSearch,
      accent: 'text-rose-600 dark:text-rose-300',
      sample: ['admin only', 'retention', 'redaction', 'trace'],
    },
  ]

  return (
    <section className='bg-background relative z-10 px-4 py-20 sm:px-6 md:py-28'>
      <div className='mx-auto max-w-7xl'>
        <AnimateInView className='border-border mb-12 max-w-3xl border-b pb-8'>
          <div className='flex items-center gap-3'>
            <span className='h-px w-8 bg-emerald-500/70' />
            <p className='text-muted-foreground text-[11px] font-semibold tracking-[0.28em] uppercase'>
              {t('Core capabilities')}
            </p>
          </div>
          <h2 className='mt-4 font-serif text-4xl leading-[1.05] font-black tracking-[-0.02em] md:text-[3.25rem]'>
            {t('Built for AI model and Agent governance')}
          </h2>
        </AnimateInView>

        <div className='border-border bg-border grid gap-px overflow-hidden rounded-xl border lg:grid-cols-3'>
          {features.map((feature, index) => (
            <FeaturePanel key={feature.title} feature={feature} index={index} />
          ))}
        </div>
      </div>
    </section>
  )
}

function FeaturePanel(props: { feature: FeatureCard; index: number }) {
  const Icon = props.feature.icon

  return (
    <AnimateInView
      delay={props.index * 70}
      animation='fade-up'
      className={cn(
        'group bg-card hover:bg-muted/30 relative overflow-hidden p-6 transition-colors md:p-7',
        props.feature.span
      )}
    >
      <span
        aria-hidden='true'
        className='editorial-numeral text-foreground/[0.045] pointer-events-none absolute -top-4 -right-1 text-[5.5rem] leading-none font-black select-none md:text-[7.5rem]'
      >
        0{props.index + 1}
      </span>
      <div className='relative flex h-full min-h-[250px] flex-col justify-between gap-8'>
        <div>
          <div className='mb-5 flex items-center gap-2.5'>
            <Icon className={cn('size-4', props.feature.accent)} />
            <span className='text-muted-foreground text-[11px] font-semibold tracking-[0.2em] uppercase'>
              {props.feature.eyebrow}
            </span>
          </div>
          <h3 className='font-serif text-2xl leading-snug font-bold tracking-[-0.01em]'>
            {props.feature.title}
          </h3>
          <p className='text-muted-foreground mt-3 text-sm leading-7'>
            {props.feature.description}
          </p>
        </div>
        <div className='flex flex-wrap gap-2'>
          {props.feature.sample.map((item) => (
            <span
              key={item}
              className='border-border bg-background text-muted-foreground rounded-md border px-2.5 py-1 font-mono text-[11px]'
            >
              {item}
            </span>
          ))}
        </div>
      </div>
      <span
        aria-hidden='true'
        className={cn(
          'absolute inset-x-0 bottom-0 h-0.5 origin-left scale-x-0 bg-current transition-transform duration-300 group-hover:scale-x-100',
          props.feature.accent
        )}
      />
    </AnimateInView>
  )
}
