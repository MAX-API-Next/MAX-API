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
import { CloudCog, KeyRound, Layers3, Route, Workflow } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { AnimateInView } from '@/components/animate-in-view'

const STEPS = [
  {
    title: 'Applications, Agents, users, and organizations',
    description:
      'Product code, internal tools, Agent workflows, user groups, and organization policies send requests through one governed entrance.',
    icon: Layers3,
    rail: 'Demand side',
  },
  {
    title: 'MAX API governance layer',
    description:
      'Authentication, token scope, model range, channel routing, quota pre-consume, billing expressions, logs, audit controls, and admin operations live here.',
    icon: KeyRound,
    rail: 'Control plane',
  },
  {
    title: 'Policy-aware routing and protocol conversion',
    description:
      'Requests are mapped to model aliases, provider channels, retries, failover rules, task polling flows, response handling, and settlement events.',
    icon: Route,
    rail: 'Execution',
  },
  {
    title: 'Upstream model and multimodal platforms',
    description:
      'OpenAI, Claude, Gemini, Azure, AWS, Vertex, Ollama, domestic models, image, audio, video, embedding, rerank, and compatible API ecosystems stay replaceable.',
    icon: CloudCog,
    rail: 'Supply side',
  },
] as const

const GOVERNANCE_OUTPUTS = [
  {
    label: 'Access',
    value: 'models, users, groups, tokens',
  },
  {
    label: 'Routing',
    value: 'weights, retries, fallback, protocol mapping',
  },
  {
    label: 'Settlement',
    value: 'quota, rate-card, expressions, refunds',
  },
  {
    label: 'Audit',
    value: 'logs, retention, admin visibility, limits',
  },
] as const

export function HowItWorks() {
  const { t } = useTranslation()

  return (
    <section className='home-section home-section--flow px-4 py-16 sm:px-6 md:py-24'>
      <div className='mx-auto grid max-w-7xl gap-8 lg:grid-cols-[0.72fr_1.28fr]'>
        <AnimateInView className='home-flow-copy'>
          <div className='home-section-kicker'>
            <Workflow aria-hidden='true' />
            {t('Position in the stack')}
          </div>
          <h2>{t('A governance layer between demand and model supply')}</h2>
          <p>
            {t(
              'MAX API does not replace model vendors or Agent orchestration frameworks. It gives production AI systems a stable infrastructure layer for access, policy, cost, observability, and operational control.'
            )}
          </p>
          <div className='home-output-grid'>
            {GOVERNANCE_OUTPUTS.map((output) => (
              <div key={output.label} className='home-output-item'>
                <span>{t(output.label)}</span>
                <strong>{t(output.value)}</strong>
              </div>
            ))}
          </div>
        </AnimateInView>

        <div className='home-flow-stack'>
          {STEPS.map((step, index) => {
            const Icon = step.icon
            return (
              <AnimateInView
                key={step.title}
                delay={index * 80}
                animation='fade-left'
                className='home-glass-panel home-flow-card'
              >
                <div className='home-flow-index'>
                  {String(index + 1).padStart(2, '0')}
                </div>
                <div className='home-icon-frame'>
                  <Icon aria-hidden='true' />
                </div>
                <div>
                  <span className='home-flow-rail'>{t(step.rail)}</span>
                  <h3>{t(step.title)}</h3>
                  <p>{t(step.description)}</p>
                </div>
              </AnimateInView>
            )
          })}
        </div>
      </div>
    </section>
  )
}
