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
  Gauge,
  KeyRound,
  ListChecks,
  LockKeyhole,
  RadioTower,
  Workflow,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { AnimateInView } from '@/components/animate-in-view'

const FEATURES = [
  {
    title: 'Unified AI model entrance',
    description:
      'Route applications through one access layer for OpenAI-compatible APIs, Responses, Claude Messages, Gemini, Azure, Bedrock, Vertex, Ollama, embeddings, rerank, images, audio, realtime, and model discovery.',
    eyebrow: 'Access',
    icon: RadioTower,
    samples: ['Chat interfaces', 'Responses APIs', 'Multimodal endpoints'],
  },
  {
    title: 'Domestic and global platform adaptation',
    description:
      'Track fast-moving Chinese and international model ecosystems through channel configuration, model mapping, protocol conversion, pricing rules, and task adapters.',
    eyebrow: 'Ecosystem',
    icon: Binary,
    samples: ['Qwen', 'DeepSeek', 'Gemini'],
  },
  {
    title: 'AgentOps governance',
    description:
      'Give Agents scoped tokens, model limits, quotas, routing policy, request traceability, failure attribution, and admin-visible audit boundaries.',
    eyebrow: 'AgentOps',
    icon: KeyRound,
    samples: ['Token scope', 'Quota limits', 'Request traces'],
  },
  {
    title: 'Billing and cost audit',
    description:
      'Model token prices, cache discounts, staged task prices, video rate-cards, pre-consume rules, refunds, and user, token, model, channel, and group attribution.',
    eyebrow: 'Settlement',
    icon: CircleDollarSign,
    samples: ['Pricing expressions', 'Task rate-cards', 'Refund rules'],
  },
  {
    title: 'Channel capability matrix',
    description:
      'Expose supported capabilities and validate Base URLs, API keys, model lists, JSON configuration, Vertex regions, Codex credentials, and task path placeholders before they fail in production.',
    eyebrow: 'Configuration',
    icon: Gauge,
    samples: ['Capability checks', 'Config validation', 'Limit hints'],
  },
  {
    title: 'Async multimodal task protocol',
    description:
      'Normalize submit paths, query paths, task IDs, progress fields, status mapping, error extraction, proxied results, and task-specific pricing for video and other long-running AI jobs.',
    eyebrow: 'Tasks',
    icon: FileSearch,
    samples: ['Task submit', 'Status polling', 'Result proxy'],
  },
  {
    title: 'Routing resilience and fallback',
    description:
      'Apply group routing, channel weights, model aliases, retry policy, upstream health, pre-consume decisions, and fallback paths before users see provider instability.',
    eyebrow: 'Reliability',
    icon: ListChecks,
    samples: ['Weighted channels', 'Retry policy', 'Health fallback'],
  },
  {
    title: 'Self-hosted security boundary',
    description:
      'Keep keys, users, organizations, groups, model scope, request logs, admin access, retention choices, and operational policy inside an infrastructure layer your team controls.',
    eyebrow: 'Deployment',
    icon: LockKeyhole,
    samples: ['Key custody', 'Log retention', 'Policy boundary'],
  },
] as const

export function Features() {
  const { t } = useTranslation()

  return (
    <section className='home-section px-4 py-16 sm:px-6 md:py-24'>
      <div className='mx-auto max-w-7xl'>
        <AnimateInView className='home-section-heading'>
          <div className='home-section-kicker'>
            <Workflow aria-hidden='true' />
            {t('Governance matrix')}
          </div>
          <h2>{t('What MAX API governs in production')}</h2>
          <p>
            {t(
              'MAX API is not only a proxy. It is the layer where model access, Agent boundaries, routing resilience, settlement, configuration quality, async tasks, logs, and private deployment rules are made explicit.'
            )}
          </p>
        </AnimateInView>

        <div className='home-feature-grid'>
          {FEATURES.map((feature, index) => {
            const Icon = feature.icon
            return (
              <AnimateInView
                key={feature.title}
                delay={index * 60}
                className='home-glass-panel home-feature-card'
              >
                <div className='home-feature-card-top'>
                  <div className='home-icon-frame'>
                    <Icon aria-hidden='true' />
                  </div>
                  <span>{t(feature.eyebrow)}</span>
                </div>
                <h3>{t(feature.title)}</h3>
                <p>{t(feature.description)}</p>
                <div className='home-chip-row'>
                  {feature.samples.map((sample) => (
                    <span key={sample}>{t(sample)}</span>
                  ))}
                </div>
              </AnimateInView>
            )
          })}
        </div>
      </div>
    </section>
  )
}
