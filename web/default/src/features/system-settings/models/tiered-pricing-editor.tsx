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
import { memo, useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  buildRequestRuleExpr,
  combineBillingExpr,
  splitBillingExprAndRequestRules,
  tryParseRequestRuleExpr,
  type RequestRuleGroup,
} from '@/features/pricing/lib/billing-expr'
import {
  type VisualConfig,
  createDefaultVisualConfig,
  generateExprFromVisualConfig,
  tryParseVisualConfig,
} from '@/features/pricing/lib/tier-expr'
import { RequestRuleEditor } from './tiered-pricing-request-rules'
import {
  CostEstimator,
  LlmPromptHelper,
  PresetSection,
  type Preset,
} from './tiered-pricing-support'
import { RawExprEditor, VisualEditor } from './tiered-pricing-visual-editor'

// ---------------------------------------------------------------------------
// Main editor
// ---------------------------------------------------------------------------

export type TieredPricingEditorProps = {
  modelName?: string
  modelIdentity?: string
  billingExpr: string
  requestRuleExpr: string
  onBillingExprChange: (next: string) => void
  onRequestRuleExprChange: (next: string) => void
}

type EditorMode = 'visual' | 'raw'

type RawExprFallback = {
  rawExpr: string
  billingExpr: string
  requestRuleExpr: string
}

type InitialEditorState = {
  editorMode: EditorMode
  visualConfig: VisualConfig | null
  rawExpr: string
  requestRuleGroups: RequestRuleGroup[]
  requestRulesCompatible: boolean
  rawExprFallback: RawExprFallback | null
}

function createInitialEditorState(
  billingExpr: string,
  requestRuleExpr: string
): InitialEditorState {
  const parsedConfig = tryParseVisualConfig(billingExpr)
  const parsedRequestRuleGroups = tryParseRequestRuleExpr(requestRuleExpr)
  const rawExpr = combineBillingExpr(billingExpr || '', requestRuleExpr || '')
  return {
    editorMode: parsedConfig || !billingExpr ? 'visual' : 'raw',
    visualConfig:
      parsedConfig || (!billingExpr ? createDefaultVisualConfig() : null),
    rawExpr,
    requestRuleGroups: parsedRequestRuleGroups ?? [],
    requestRulesCompatible: parsedRequestRuleGroups !== null,
    rawExprFallback:
      parsedRequestRuleGroups === null && billingExpr && requestRuleExpr
        ? { rawExpr, billingExpr, requestRuleExpr }
        : null,
  }
}

function splitRawExprWithFallback(
  rawExpr: string,
  fallback: RawExprFallback | null
) {
  if (fallback?.rawExpr === rawExpr) {
    return {
      billingExpr: fallback.billingExpr,
      requestRuleExpr: fallback.requestRuleExpr,
    }
  }
  return splitBillingExprAndRequestRules(rawExpr)
}

const TieredPricingEditorContent = memo(function TieredPricingEditorContent({
  modelName,
  billingExpr: currentExpr,
  requestRuleExpr: currentRequestRuleExpr,
  onBillingExprChange,
  onRequestRuleExprChange,
}: TieredPricingEditorProps) {
  const { t } = useTranslation()
  const [initialState] = useState(() =>
    createInitialEditorState(currentExpr, currentRequestRuleExpr)
  )
  const [editorMode, setEditorMode] = useState<EditorMode>(
    initialState.editorMode
  )
  const [visualConfig, setVisualConfig] = useState<VisualConfig | null>(
    initialState.visualConfig
  )
  const [rawExpr, setRawExpr] = useState(initialState.rawExpr)
  const [requestRuleGroups, setRequestRuleGroups] = useState<
    RequestRuleGroup[]
  >(initialState.requestRuleGroups)
  const [requestRulesCompatible, setRequestRulesCompatible] = useState(
    initialState.requestRulesCompatible
  )
  const [rawExprFallback, setRawExprFallback] =
    useState<RawExprFallback | null>(initialState.rawExprFallback)

  const effectiveExpr = useMemo(() => {
    if (editorMode === 'visual') {
      return generateExprFromVisualConfig(visualConfig)
    }
    const { billingExpr } = splitRawExprWithFallback(rawExpr, rawExprFallback)
    return billingExpr
  }, [editorMode, visualConfig, rawExpr, rawExprFallback])

  useEffect(() => {
    if (effectiveExpr !== currentExpr) {
      onBillingExprChange(effectiveExpr)
    }
  }, [effectiveExpr, currentExpr, onBillingExprChange])

  useEffect(() => {
    if (editorMode !== 'visual' || !requestRulesCompatible) return
    const ruleExpr = buildRequestRuleExpr(requestRuleGroups)
    if (ruleExpr !== currentRequestRuleExpr) {
      onRequestRuleExprChange(ruleExpr)
    }
  }, [
    editorMode,
    requestRuleGroups,
    requestRulesCompatible,
    currentRequestRuleExpr,
    onRequestRuleExprChange,
  ])

  const handleVisualChange = useCallback((next: VisualConfig) => {
    setVisualConfig(next)
  }, [])

  const handleRawChange = useCallback(
    (value: string) => {
      setRawExprFallback(null)
      setRawExpr(value)
      const { requestRuleExpr: ruleStr } =
        splitBillingExprAndRequestRules(value)
      setRequestRulesCompatible(tryParseRequestRuleExpr(ruleStr) !== null)
      onRequestRuleExprChange(ruleStr)
    },
    [onRequestRuleExprChange]
  )

  const handleModeChange = useCallback(
    (next: EditorMode) => {
      if (next === 'visual') {
        const { billingExpr, requestRuleExpr: ruleStr } =
          splitRawExprWithFallback(rawExpr, rawExprFallback)
        setRawExprFallback(null)
        const parsed = tryParseVisualConfig(billingExpr)
        if (parsed) {
          setVisualConfig(parsed)
        } else {
          setVisualConfig(createDefaultVisualConfig())
        }
        const parsedGroups = tryParseRequestRuleExpr(ruleStr)
        if (parsedGroups !== null) {
          setRequestRuleGroups(parsedGroups)
          setRequestRulesCompatible(true)
        } else {
          setRequestRulesCompatible(false)
        }
        onRequestRuleExprChange(ruleStr)
      } else {
        const expr = generateExprFromVisualConfig(visualConfig)
        const ruleExpr = requestRulesCompatible
          ? buildRequestRuleExpr(requestRuleGroups)
          : currentRequestRuleExpr
        const combined = combineBillingExpr(expr, ruleExpr) || expr
        setRawExpr(combined)
        setRawExprFallback(
          !requestRulesCompatible && ruleExpr
            ? {
                rawExpr: combined,
                billingExpr: expr,
                requestRuleExpr: ruleExpr,
              }
            : null
        )
      }
      setEditorMode(next)
    },
    [
      rawExpr,
      rawExprFallback,
      visualConfig,
      requestRuleGroups,
      requestRulesCompatible,
      currentRequestRuleExpr,
      onRequestRuleExprChange,
    ]
  )

  const applyPreset = useCallback(
    (preset: Preset) => {
      const presetGroups = preset.requestRules || []
      const ruleExpr = buildRequestRuleExpr(presetGroups)
      const combined = combineBillingExpr(preset.expr, ruleExpr) || preset.expr
      setRawExpr(combined)
      const parsed = tryParseVisualConfig(preset.expr)
      if (parsed) {
        setVisualConfig(parsed)
        setEditorMode('visual')
      } else {
        setEditorMode('raw')
        setVisualConfig(null)
      }
      setRequestRuleGroups(presetGroups)
      setRequestRulesCompatible(true)
      setRawExprFallback(null)
      onRequestRuleExprChange(ruleExpr)
    },
    [onRequestRuleExprChange]
  )

  const handleRuleGroupsChange = useCallback((next: RequestRuleGroup[]) => {
    setRequestRuleGroups(next)
    setRequestRulesCompatible(true)
  }, [])

  return (
    <div className='space-y-4'>
      <div className='flex items-center justify-between gap-2'>
        <Label className='text-xs'>{t('Editor mode')}</Label>
        <Select
          items={[
            { value: 'visual', label: t('Visual editor') },
            { value: 'raw', label: t('Expression editor') },
          ]}
          value={editorMode}
          onValueChange={(value) => handleModeChange(value as EditorMode)}
        >
          <SelectTrigger className='w-44' size='sm'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              <SelectItem value='visual'>{t('Visual editor')}</SelectItem>
              <SelectItem value='raw'>{t('Expression editor')}</SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>

      <div className='flex flex-wrap items-start gap-x-4 gap-y-1'>
        <div className='flex-1'>
          <PresetSection applyPreset={applyPreset} />
        </div>
        {editorMode === 'raw' && <LlmPromptHelper modelName={modelName} />}
      </div>

      <div className='bg-muted/30 space-y-3 rounded-md border p-3'>
        {editorMode === 'visual' ? (
          <VisualEditor
            visualConfig={visualConfig}
            onChange={handleVisualChange}
          />
        ) : (
          <RawExprEditor exprString={rawExpr} onChange={handleRawChange} />
        )}

        {editorMode === 'visual' && (
          <RequestRuleEditor
            requestRuleExpr={currentRequestRuleExpr}
            groups={requestRuleGroups}
            onChange={handleRuleGroupsChange}
          />
        )}
      </div>

      <CostEstimator effectiveExpr={effectiveExpr} />
    </div>
  )
})

export const TieredPricingEditor = memo(function TieredPricingEditor(
  props: TieredPricingEditorProps
) {
  const identity =
    props.modelIdentity ?? props.modelName ?? '__new-tiered-pricing-editor__'

  return <TieredPricingEditorContent key={identity} {...props} />
})
