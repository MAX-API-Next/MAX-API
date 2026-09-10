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
import type {
  TaskRateCardPricing,
  TaskRateCardPricingComponent,
} from '../types'

export function hasStructuredTaskRateCard(
  card: TaskRateCardPricing | null | undefined
): boolean {
  return Boolean(card?.billing_type && (card.components?.length ?? 0) > 0)
}

export function getTaskRateCardComponentLabelKey(
  component: TaskRateCardPricingComponent
): string | null {
  if (component.key === 'output_video' && component.variant === '768P') {
    return 'Output video / 768P'
  }
  if (component.key === 'output_video' && component.variant === '2K') {
    return 'Output video / 2K'
  }
  if (component.key === 'input_video' && component.variant === '768P') {
    return 'Input video / 768P'
  }
  if (component.key === 'input_video' && component.variant === '2K') {
    return 'Input video / 2K'
  }
  if (component.key === 'input_image') {
    return 'Extra input image price'
  }
  if (component.key === 'input_audio') {
    return 'Input audio price'
  }
  return null
}
