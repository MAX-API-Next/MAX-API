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
import { ChartNoAxesCombined, KeyRound, Route, Workflow } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { AnimateInView } from '@/components/animate-in-view'

export function HowItWorks() {
  const { t } = useTranslation()
  const steps = [
    {
      title: t('Connect authorized upstreams'),
      description: t(
        'Add authorized provider keys, model catalogs, model mappings, custom paths, task protocols, and channel groups.'
      ),
      icon: Route,
    },
    {
      title: t('Govern users, Agents, and tokens'),
      description: t(
        'Create scoped access tokens for users, apps, Agents, and workflows, then set model limits, quotas, groups, request limits, and audit policy.'
      ),
      icon: KeyRound,
    },
    {
      title: t('Operate AI and Agent workloads'),
      description: t(
        'Observe model usage, Agent cost, latency, errors, retries, async task state, and admin-scoped audit boundaries.'
      ),
      icon: ChartNoAxesCombined,
    },
    {
      title: t('Iterate with platform changes'),
      description: t(
        'Update model configs, pricing expressions, rate-cards, and protocol templates as upstreams evolve.'
      ),
      icon: Workflow,
    },
  ]

  return (
    <section className='border-border bg-muted/20 relative z-10 border-y px-4 py-20 sm:px-6 md:py-28'>
      <div className='mx-auto max-w-7xl'>
        <div className='grid gap-12 lg:grid-cols-[0.78fr_1.22fr]'>
          <AnimateInView className='lg:sticky lg:top-28 lg:self-start'>
            <div className='flex items-center gap-3'>
              <span className='h-px w-8 bg-emerald-500/70' />
              <p className='text-muted-foreground text-[11px] font-semibold tracking-[0.28em] uppercase'>
                {t('Operating lifecycle')}
              </p>
            </div>
            <h2 className='mt-4 font-serif text-4xl leading-[1.05] font-black tracking-[-0.02em] md:text-[3.25rem]'>
              {t('From AI model access to Agent lifecycle governance')}
            </h2>
            <p className='text-muted-foreground mt-5 max-w-md text-sm leading-7 md:text-base'>
              {t(
                'A service layer for the continuous operation of AI models and Agents, adapting as model platforms, pricing rules, Agent workloads, and audit requirements evolve.'
              )}
            </p>
          </AnimateInView>

          <div className='relative'>
            <span
              aria-hidden='true'
              className='bg-border absolute top-0 bottom-0 left-[2.15rem] hidden w-px sm:block'
            />
            <div className='flex flex-col'>
              {steps.map((step, index) => {
                const Icon = step.icon
                return (
                  <AnimateInView
                    key={step.title}
                    delay={index * 90}
                    animation='fade-left'
                    className='group border-border relative flex gap-5 border-b py-6 last:border-b-0'
                  >
                    <div className='border-border bg-background relative z-10 flex size-[4.3rem] shrink-0 flex-col items-center justify-center rounded-xl border transition-colors group-hover:border-emerald-500/50'>
                      <span className='editorial-numeral text-foreground text-2xl font-black'>
                        {String(index + 1).padStart(2, '0')}
                      </span>
                    </div>
                    <div className='pt-1'>
                      <div className='mb-2 flex items-center gap-2'>
                        <Icon className='size-4 text-emerald-700 dark:text-emerald-300' />
                        <h3 className='font-serif text-xl font-bold tracking-[-0.01em]'>
                          {step.title}
                        </h3>
                      </div>
                      <p className='text-muted-foreground text-sm leading-7'>
                        {step.description}
                      </p>
                    </div>
                  </AnimateInView>
                )
              })}
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}
