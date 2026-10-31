// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import {
  ModelPricingError,
  modelPricingTransport,
  type ModelPricingTransport,
} from './model-pricing-api'
import {
  modelPricingSampleSchema,
  normalizePricing,
  pricingChange,
  pricingEqual,
  samePricingGuards,
  modelPricingNumericKeys,
  type ModelPricingEntry,
  type ModelPricingValues,
  type ModelPricingSample,
  type ModelPricingQuote,
  type ModelPricingQuoteRequest,
  type GuardedPricingChange,
} from './model-pricing-types'

type Failure = { kind: string; message: string }
type Write = {
  pricing?: ModelPricingValues // Unknown reset targets cannot be inferred from effective values.
  revision: number
  acknowledged: boolean
}
type Document = {
  base?: ModelPricingEntry
  pricing?: ModelPricingValues
  inputs?: Record<string, string>
  texts?: Record<string, string>
  revision: number
  source: string
  remote?: ModelPricingEntry
  loading: boolean
  writeState: 'idle' | 'saving' | 'unknown' | 'conflict'
  cachePublished?: boolean
  warning?: string
  commitKnown: boolean
  error?: Failure
  preview?: ModelPricingEntry
  sample?: ModelPricingSample
  sampleText?: string
  quote?: { request: ModelPricingQuoteRequest; result: ModelPricingQuote }
  readId: number
  previewId: number
  quoteId: number
  readAbort?: AbortController
  previewAbort?: AbortController
  quoteAbort?: AbortController
  write?: Write
}
export type PricingDraftView = Pick<
  Document,
  | 'base'
  | 'pricing'
  | 'inputs'
  | 'texts'
  | 'revision'
  | 'source'
  | 'remote'
  | 'loading'
  | 'writeState'
  | 'cachePublished'
  | 'warning'
  | 'commitKnown'
  | 'error'
  | 'preview'
  | 'sample'
  | 'sampleText'
  | 'quote'
> & { dirty: boolean }
export type PricingDraftSnapshot = {
  model: string | null
  document?: PricingDraftView
  blockedModels?: string[]
  cacheWarningModels?: string[]
}
const failure = (e: unknown): Failure => ({
  kind: e instanceof ModelPricingError ? e.kind : 'request-failed',
  message: e instanceof Error ? e.message : 'Request failed',
})
function freezeView<T>(value: T): T {
  if (value && typeof value === 'object') {
    for (const child of Object.values(value)) freezeView(child)
    Object.freeze(value)
  }
  return value
}

// Per-model drafts reference authoritative entries; this is neither another
// price store nor a calculator. Request IDs order local operations only. Server
// hashes are equality guards. Refresh never implicitly rebases an edited draft.
export class ModelPricingDraftSession {
  private documents = new Map<string, Document>()
  private model: string | null = null
  private enabled = true
  private listeners = new Set<() => void>()
  private snapshot: PricingDraftSnapshot = { model: null }
  private readonly inactiveSnapshot: PricingDraftSnapshot = Object.freeze({
    model: null,
  })
  constructor(
    private readonly transport: ModelPricingTransport = modelPricingTransport,
    private readonly isCurrent: () => boolean = () => true
  ) {}
  getSnapshot = (): PricingDraftSnapshot =>
    this.isCurrent() ? this.snapshot : this.inactiveSnapshot
  private live() {
    return this.enabled && this.isCurrent()
  }
  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }
  private emit() {
    const d = this.model === null ? undefined : this.documents.get(this.model)
    let document: PricingDraftView | undefined
    if (d) {
      const {
        base,
        pricing,
        inputs,
        texts,
        revision,
        source,
        remote,
        loading,
        writeState,
        cachePublished,
        warning,
        commitKnown,
        error,
        preview,
        sample,
        sampleText,
        quote,
      } = d
      document = structuredClone({
        base,
        pricing,
        inputs,
        texts,
        revision,
        source,
        remote,
        loading,
        writeState,
        cachePublished,
        warning,
        commitKnown,
        error,
        preview,
        sample,
        sampleText,
        quote,
        dirty: Boolean(
          (base && pricing && !pricingEqual(base.configured, pricing)) ||
          Object.keys(inputs ?? {}).length
        ),
      })
    }
    this.snapshot = freezeView({
      model: this.model,
      document,
      blockedModels: [...this.documents]
        .filter(([, value]) => value.writeState !== 'idle')
        .map(([name]) => name),
      cacheWarningModels: [...this.documents]
        .filter(([, value]) => value.cachePublished === false)
        .map(([name]) => name),
    })
    for (const listener of this.listeners) listener()
  }
  private current(): [string, Document] {
    if (!this.live() || this.model === null)
      throw new Error('Pricing session is inactive')
    const d = this.documents.get(this.model)
    if (!d) throw new Error('Pricing model is unavailable')
    return [this.model, d]
  }
  private cancel(d: Document) {
    d.readId++
    d.previewId++
    d.quoteId++
    d.readAbort?.abort()
    d.previewAbort?.abort()
    d.quoteAbort?.abort()
    d.loading = false
  }
  resume() {
    this.enabled = true
  }
  suspend() {
    this.enabled = false
    for (const d of this.documents.values()) this.cancel(d)
    this.emit()
  }
  select(model: string) {
    if (model === this.model) return
    if (this.model !== null) {
      const previous = this.documents.get(this.model)
      if (previous) this.cancel(previous)
    }
    if (!model.trim()) throw new Error('Pricing model is required')
    this.model = model
    if (!this.documents.has(model))
      this.documents.set(model, {
        revision: 0,
        source: '',
        loading: false,
        writeState: 'idle',
        commitKnown: false,
        readId: 0,
        previewId: 0,
        quoteId: 0,
      })
    this.emit()
  }
  private invalidateSamples(d: Document) {
    d.previewId++
    d.quoteId++
    d.previewAbort?.abort()
    d.quoteAbort?.abort()
    d.preview = undefined
    d.quote = undefined
  }
  replacePricing(pricing: ModelPricingValues) {
    const [, d] = this.current()
    if (!d.base) throw new Error('Load authoritative pricing before editing')
    const next = normalizePricing(pricing)
    for (const key of modelPricingNumericKeys) {
      const text = d.texts?.[key]
      if (
        text !== undefined &&
        (text === '' ? undefined : Number(text)) !== next[key]
      )
        delete d.texts![key]
    }
    d.pricing = next
    d.revision++
    this.invalidateSamples(d)
    this.emit()
  }
  editNumber(key: (typeof modelPricingNumericKeys)[number], text: string) {
    const [, d] = this.current()
    if (!d.pricing || !modelPricingNumericKeys.includes(key))
      throw new Error('Load pricing before editing')
    d.inputs ??= {}
    const value = Number(text)
    if (
      text !== '' &&
      (!text.trim() ||
        !Number.isFinite(value) ||
        value < 0 ||
        /[.eE+-]$/.test(text))
    ) {
      d.inputs[key] = text
      d.revision++
      this.invalidateSamples(d)
      this.emit()
      return
    }
    delete d.inputs[key]
    d.texts = { ...d.texts, [key]: text }
    const next = { ...d.pricing }
    if (text === '') delete next[key]
    else next[key] = value
    this.replacePricing(next)
  }
  importPricing(text: string) {
    const [, d] = this.current()
    let value: ModelPricingValues
    try {
      value = normalizePricing(JSON.parse(text))
    } catch {
      d.inputs = { ...d.inputs, $json: text }
      d.revision++
      this.invalidateSamples(d)
      this.emit()
      return false
    }
    d.inputs = {}
    d.texts = {}
    this.replacePricing(value)
    return true
  }
  selectSource(source: string) {
    const [, d] = this.current()
    if (source === d.source) return
    d.source = source
    d.quoteId++
    d.quoteAbort?.abort()
    d.quote = undefined
    this.emit()
  }
  setSample(sample: ModelPricingSample) {
    const [, d] = this.current()
    d.sample = modelPricingSampleSchema.parse(sample)
    d.sampleText = undefined
    d.quoteId++
    d.quoteAbort?.abort()
    d.quote = undefined
    this.emit()
  }
  editSample(text: string) {
    const [, d] = this.current()
    d.sampleText = text
    try {
      d.sample = modelPricingSampleSchema.parse(JSON.parse(text))
    } catch {
      d.sample = undefined
    }
    d.quoteId++
    d.quoteAbort?.abort()
    d.quote = undefined
    this.emit()
  }
  discardLocal() {
    const [, d] = this.current()
    if (!d.base || d.writeState !== 'idle')
      throw new Error('Refresh and reconcile pricing before discarding')
    d.pricing = normalizePricing(d.base.configured)
    d.inputs = {}
    d.texts = {}
    d.error = undefined
    d.revision++
    this.invalidateSamples(d)
    this.emit()
  }
  private applyRead(d: Document, entry: ModelPricingEntry, revision: number) {
    if (d.write) {
      if (d.write.pricing && pricingEqual(entry.configured, d.write.pricing)) {
        if (d.revision === d.write.revision) {
          d.pricing = normalizePricing(entry.configured)
          d.texts = {}
        }
        d.base = structuredClone(entry)
        d.commitKnown = d.write.acknowledged
        d.write = undefined
        d.writeState = 'idle'
        d.remote = undefined
        d.error = undefined
        d.revision++
        this.invalidateSamples(d)
      } else {
        d.remote = structuredClone(entry)
        d.writeState = d.write.acknowledged ? 'conflict' : 'unknown'
      }
      return
    }
    const edited =
      d.revision !== revision ||
      Object.keys(d.inputs ?? {}).length > 0 ||
      Object.keys(d.texts ?? {}).length > 0 ||
      Boolean(
        d.base && d.pricing && !pricingEqual(d.base.configured, d.pricing)
      )
    if (edited) {
      if (
        !d.base ||
        !samePricingGuards(d.base, entry) ||
        !pricingEqual(d.base.configured, entry.configured)
      ) {
        d.remote = structuredClone(entry)
        d.writeState = 'conflict'
        this.invalidateSamples(d)
      } else {
        d.remote = undefined
        d.writeState = 'idle'
      }
      return
    }
    d.base = structuredClone(entry)
    d.pricing = normalizePricing(entry.configured)
    d.inputs = {}
    d.texts = {}
    d.remote = undefined
    d.writeState = 'idle'
    d.revision++
    this.invalidateSamples(d)
  }
  private async read(model: string, d: Document): Promise<boolean> {
    const id = ++d.readId,
      revision = d.revision
    d.readAbort?.abort()
    d.readAbort = new AbortController()
    d.loading = true
    this.emit()
    try {
      const entry = await this.transport.read(model, d.readAbort.signal)
      if (!this.live() || id !== d.readId) return false
      if (entry.model_name !== model)
        throw new ModelPricingError(
          'invalid-response',
          'Mismatched pricing model'
        )
      d.error = undefined
      this.applyRead(d, entry, revision)
      return d.writeState === 'idle'
    } catch (e) {
      if (this.live() && id === d.readId) d.error = failure(e)
      return false
    } finally {
      if (this.live() && id === d.readId) {
        d.loading = false
        this.emit()
      }
    }
  }
  async refresh(): Promise<boolean> {
    const [model, d] = this.current()
    if (d.writeState === 'saving') return false
    return this.read(model, d)
  }
  // Explicit discard of local work, never called by refresh or a failed request.
  // Adopting a read does not establish that an ambiguous earlier PATCH failed.
  adoptRemote() {
    const [, d] = this.current()
    if (d.writeState === 'saving' || !d.remote)
      throw new Error('No server snapshot available to adopt')
    d.base = structuredClone(d.remote)
    d.pricing = normalizePricing(d.remote.configured)
    d.inputs = {}
    d.texts = {}
    d.remote = undefined
    d.write = undefined
    d.writeState = 'idle'
    d.error = undefined
    d.revision++
    this.invalidateSamples(d)
    this.emit()
  }
  async save(reset = false): Promise<boolean> {
    const [model, d] = this.current()
    if (
      !d.base ||
      !d.pricing ||
      d.loading ||
      d.writeState !== 'idle' ||
      Object.keys(d.inputs ?? {}).length
    )
      throw new Error('Reconcile authoritative pricing before saving')
    if (!reset && pricingEqual(d.base.configured, d.pricing)) return false
    const change = reset
      ? { ...pricingChange(d.base, {}), reset: true }
      : pricingChange(d.base, d.pricing)
    return this.commit([{ model, d, change }])
  }
  // A deliberate full copy/reset. Existing dirty target drafts are never discarded.
  // Entries are the caller's observed authority, not fresh guards silently read at save.
  async saveBatch(
    entries: ModelPricingEntry[],
    pricing?: ModelPricingValues
  ): Promise<boolean> {
    this.current()
    if (
      !entries.length ||
      entries.length > 200 ||
      new Set(entries.map((e) => e.model_name)).size !== entries.length
    )
      throw new Error('Select between 1 and 200 distinct models')
    if (entries.length > 1 && !this.transport.saveMany)
      throw new Error('Batch pricing transport is unavailable')
    const batch = entries.map((entry) => {
      const d = this.documents.get(entry.model_name)
      if (
        d &&
        (d.loading ||
          d.writeState !== 'idle' ||
          Object.keys(d.inputs ?? {}).length ||
          (d.base && d.pricing && !pricingEqual(d.base.configured, d.pricing)))
      )
        throw new Error(
          'Reconcile or discard target drafts before batch pricing'
        )
      if (d?.base && !samePricingGuards(d.base, entry))
        throw new Error('Refresh the model list before batch pricing')
      const change =
        pricing === undefined
          ? { ...pricingChange(entry, {}), reset: true }
          : pricingChange(entry, pricing)
      return {
        model: entry.model_name,
        d: d ?? {
          revision: 0,
          source: '',
          loading: false,
          writeState: 'idle' as const,
          commitKnown: false,
          readId: 0,
          previewId: 0,
          quoteId: 0,
        },
        entry,
        change,
      }
    })
    for (const item of batch) {
      item.d.base = structuredClone(item.entry)
      item.d.pricing = normalizePricing(pricing ?? item.entry.configured)
      item.d.texts = {}
      item.d.revision++
      this.documents.set(item.model, item.d)
    }
    return this.commit(batch)
  }
  async reconcileModels(models: string[]): Promise<boolean> {
    this.current()
    let complete = true
    for (const model of models) {
      const d = this.documents.get(model)
      if (d && d.writeState !== 'saving')
        complete = (await this.read(model, d)) && complete
      if (!this.live()) return false
    }
    return complete
  }
  private async commit(
    batch: { model: string; d: Document; change: GuardedPricingChange }[]
  ): Promise<boolean> {
    const writes = batch.map((item) => {
      const write: Write = {
        pricing: item.change.reset
          ? undefined
          : structuredClone(item.change.pricing),
        revision: item.d.revision,
        acknowledged: false,
      }
      item.d.write = write
      item.d.writeState = 'saving'
      item.d.commitKnown = false
      item.d.cachePublished = undefined
      item.d.warning = undefined
      this.cancel(item.d)
      this.invalidateSamples(item.d)
      return { ...item, write }
    })
    this.emit()
    try {
      const receipt =
        batch.length === 1
          ? await this.transport.save(batch[0].change)
          : await this.transport.saveMany!(batch.map((item) => item.change))
      const entries = new Map(
        receipt.entries.map((entry) => [entry.model_name, entry])
      )
      if (
        receipt.entries.length !== writes.length ||
        entries.size !== writes.length ||
        writes.some(
          ({ model, write }) =>
            !entries.has(model) ||
            (write.pricing &&
              !pricingEqual(entries.get(model)!.configured, write.pricing))
        )
      )
        throw new ModelPricingError(
          'invalid-response',
          'Mismatched pricing receipt'
        )
      for (const { model, d, write } of writes) {
        if (d.write !== write) continue
        write.pricing = normalizePricing(entries.get(model)!.configured)
        write.acknowledged = true
        d.commitKnown = true
        d.cachePublished = receipt.cache_published
        d.warning = receipt.warning
      }
    } catch (e) {
      const rejected =
        e instanceof ModelPricingError &&
        (e.kind === 'rejected' || e.kind === 'conflict')
      for (const { d, write } of writes) {
        if (d.write !== write) continue
        d.error = failure(e)
        if (rejected) {
          d.write = undefined
          d.writeState = e.kind === 'conflict' ? 'conflict' : 'idle'
        }
      }
      if (rejected) {
        this.emit()
        return false
      }
    }
    for (const { d, write } of writes)
      if (d.write === write) d.writeState = 'unknown'
    this.emit()
    if (!this.live()) return false
    return this.reconcileModels(writes.map((item) => item.model)) // NEW GETs after PATCH.
  }
  async preview(): Promise<boolean> {
    const [model, d] = this.current()
    if (
      !d.base ||
      !d.pricing ||
      d.writeState !== 'idle' ||
      Object.keys(d.inputs ?? {}).length
    )
      return false
    const id = ++d.previewId,
      revision = d.revision,
      base = d.base
    d.previewAbort?.abort()
    d.previewAbort = new AbortController()
    d.preview = undefined
    try {
      const result = await this.transport.preview(
        model,
        normalizePricing(d.pricing),
        d.previewAbort.signal
      )
      if (!this.live() || id !== d.previewId || revision !== d.revision)
        return false
      if (!samePricingGuards(base, result))
        throw new ModelPricingError('conflict', 'Pricing context changed')
      d.preview = structuredClone(result)
      d.error = undefined
      this.emit()
      return true
    } catch (e) {
      if (id === d.previewId && this.live()) {
        d.error = failure(e)
        if (d.error.kind === 'conflict') d.writeState = 'conflict'
        this.emit()
      }
      return false
    }
  }
  async quote(): Promise<boolean> {
    const [model, d] = this.current()
    const context = d.base?.contexts.find((item) => item.id === d.source)
    if (
      !d.base ||
      !d.pricing ||
      !d.sample ||
      Object.keys(d.inputs ?? {}).length > 0 ||
      d.writeState !== 'idle' ||
      !context?.available ||
      !context.contract
    )
      return false
    const change = pricingChange(d.base, d.pricing)
    const { reset: _reset, ...guarded } = change
    const input: ModelPricingQuoteRequest = {
      ...structuredClone(d.sample),
      ...guarded,
      context_id: d.source,
    }
    const id = ++d.quoteId,
      revision = d.revision
    d.quoteAbort?.abort()
    d.quoteAbort = new AbortController()
    d.quote = undefined
    try {
      const result = await this.transport.quote(input, d.quoteAbort.signal)
      if (!this.live() || id !== d.quoteId || revision !== d.revision)
        return false
      const driver = context.plugin_key ? 'task_plugin' : 'go_adapter'
      if (
        result.price_version !== input.expected_version ||
        result.contract_digest !== input.expected_contract_digest ||
        result.binding_version !== input.expected_binding_version ||
        result.group !== input.group ||
        result.preview.source.driver !== driver ||
        (result.preview.source.plugin_key || '') !==
          (context.plugin_key || '') ||
        ![model, context.mapped_model].includes(result.preview.selection.model)
      )
        throw new ModelPricingError('conflict', 'Pricing quote context changed')
      d.quote = { request: input, result: structuredClone(result) }
      d.error = undefined
      this.emit()
      return true
    } catch (e) {
      if (id === d.quoteId && this.live()) {
        d.error = failure(e)
        if (d.error.kind === 'conflict') d.writeState = 'conflict'
        this.emit()
      }
      return false
    }
  }
}
