import {
  Activity,
  Eye,
  Loader2,
  Plus,
  RefreshCw,
  RotateCcw,
  Save,
  ShieldCheck,
  Trash2,
} from 'lucide-react'
import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { PasswordInput } from '@/components/password-input'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import {
  batchDeletePromptAuditEvents,
  deletePromptAuditEventsByFilter,
  deletePromptAuditEvent,
  getPromptAuditConfig,
  getPromptAuditEvent,
  getPromptAuditRuntime,
  listPromptAuditEvents,
  probePromptAuditEndpoint,
  previewPromptAuditEventDelete,
  updatePromptAuditConfig,
} from '../api'
import { SettingsSection } from '../components/settings-section'
import type {
  PromptAuditConfig,
  PromptAuditEndpoint,
  PromptAuditDeletePreview,
  PromptAuditEvent,
  PromptAuditEventFilter,
  PromptAuditMode,
  PromptAuditRuntime,
} from '../types'
import {
  promptAuditEndpointInputLimitRange,
  promptAuditWorkerCountRange,
  validatePromptAuditResourceLimits,
} from './lib/prompt-audit-validation'

const scanners = [
  ['violent', 'Violence'],
  ['non_violent_illegal_acts', 'Non-violent illegal acts'],
  ['sexual_content_or_sexual_acts', 'Sexual content or sexual acts'],
  ['pii', 'Personal identifying information'],
  ['suicide_and_self_harm', 'Suicide and self-harm'],
  ['unethical_acts', 'Unethical acts'],
  ['politically_sensitive_topics', 'Politically sensitive topics'],
  ['copyright_violation', 'Copyright violation'],
  ['jailbreak', 'Jailbreak and prompt injection'],
] as const

const modeOptions: Array<{ value: PromptAuditMode; label: string }> = [
  { value: 'off', label: 'Off' },
  { value: 'async_audit', label: 'Asynchronous audit' },
  { value: 'blocking', label: 'Blocking' },
]

type PromptAuditSectionProps = { groupRatio: string }

function cloneConfig(config: PromptAuditConfig): PromptAuditConfig {
  const cloned = structuredClone(config)
  Object.values(cloned.group_policies).forEach((policy) => {
    policy.mode ||= policy.enabled ? cloned.mode : 'off'
  })
  return cloned
}

function decisionBadgeVariant(decision: PromptAuditEvent['decision']) {
  if (decision === 'critical') return 'destructive' as const
  if (decision === 'flag') return 'warning' as const
  return 'outline' as const
}

function newEndpoint(index: number): PromptAuditEndpoint {
  return {
    id: `qwen3guard-${index}`,
    name: `Qwen3Guard ${index}`,
    protocol: 'openai_compatible',
    base_url: 'http://qwen3guard:11434',
    model: 'sileader/qwen3guard:0.6b',
    timeout_ms: 10000,
    input_limit: 8000,
    enabled: true,
    has_token: false,
    token_status: 'missing',
    token: '',
    clear_token: false,
  }
}

function groupNames(groupRatio: string, config: PromptAuditConfig | null) {
  const values = new Set(Object.keys(config?.group_policies ?? {}))
  try {
    const parsed = JSON.parse(groupRatio || '{}') as Record<string, unknown>
    Object.keys(parsed).forEach((group) => values.add(group))
  } catch {
    // Existing policies remain editable while the ratio option is invalid.
  }
  return [...values].sort((left, right) => left.localeCompare(right))
}

function localDateTime(value: Date) {
  const offset = value.getTimezoneOffset() * 60_000
  return new Date(value.getTime() - offset).toISOString().slice(0, 16)
}

type PromptAuditFilterDraft = {
  decision: string
  risk_level: string
  group: string
  user_id: string
  token_id: string
  model: string
  request_id: string
  prompt_hash: string
  endpoint: string
  keyword: string
  start_time: string
  end_time: string
}

const emptyPromptAuditFilter: PromptAuditFilterDraft = {
  decision: '',
  risk_level: '',
  group: '',
  user_id: '',
  token_id: '',
  model: '',
  request_id: '',
  prompt_hash: '',
  endpoint: '',
  keyword: '',
  start_time: '',
  end_time: '',
}

function eventFilterFromDraft(
  filter: PromptAuditFilterDraft
): PromptAuditEventFilter {
  const userID = Number(filter.user_id)
  const tokenID = Number(filter.token_id)
  const startTime = filter.start_time
    ? Math.floor(new Date(filter.start_time).getTime() / 1000)
    : 0
  const endTime = filter.end_time
    ? Math.floor(new Date(filter.end_time).getTime() / 1000)
    : 0
  return {
    decision: filter.decision || undefined,
    risk_level: filter.risk_level || undefined,
    group: filter.group || undefined,
    user_id: userID > 0 ? userID : undefined,
    token_id: tokenID > 0 ? tokenID : undefined,
    model: filter.model || undefined,
    request_id: filter.request_id || undefined,
    prompt_hash: filter.prompt_hash || undefined,
    endpoint: filter.endpoint || undefined,
    keyword: filter.keyword || undefined,
    start_time: startTime > 0 ? startTime : undefined,
    end_time: endTime > 0 ? endTime : undefined,
  }
}

export function PromptAuditSection({ groupRatio }: PromptAuditSectionProps) {
  const { t } = useTranslation()
  const [config, setConfig] = useState<PromptAuditConfig | null>(null)
  const [savedConfig, setSavedConfig] = useState<PromptAuditConfig | null>(null)
  const [runtime, setRuntime] = useState<PromptAuditRuntime | null>(null)
  const [events, setEvents] = useState<PromptAuditEvent[]>([])
  const [eventTotal, setEventTotal] = useState(0)
  const [eventPage, setEventPage] = useState(1)
  const [eventFilter, setEventFilter] = useState<PromptAuditFilterDraft>(
    emptyPromptAuditFilter
  )
  const [activeEvent, setActiveEvent] = useState<PromptAuditEvent | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [probing, setProbing] = useState<string[]>([])
  const [selectedEvents, setSelectedEvents] = useState<number[]>([])
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [deleteStart, setDeleteStart] = useState(() =>
    localDateTime(new Date(Date.now() - 7 * 24 * 60 * 60 * 1000))
  )
  const [deleteEnd, setDeleteEnd] = useState(() => localDateTime(new Date()))
  const [deletePreview, setDeletePreview] =
    useState<PromptAuditDeletePreview | null>(null)
  const [deleteFilter, setDeleteFilter] =
    useState<PromptAuditEventFilter | null>(null)
  const [deleting, setDeleting] = useState(false)

  const loadEvents = useCallback(async () => {
    const page = await listPromptAuditEvents({
      page: eventPage,
      page_size: 20,
      ...eventFilterFromDraft(eventFilter),
    })
    setEvents(page.items)
    setEventTotal(page.total)
  }, [eventFilter, eventPage])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [nextConfig, nextRuntime] = await Promise.all([
        getPromptAuditConfig(),
        getPromptAuditRuntime(),
      ])
      setConfig(cloneConfig(nextConfig))
      setSavedConfig(cloneConfig(nextConfig))
      setRuntime(nextRuntime)
    } catch (error) {
      toast.error(
        error instanceof Error ? t(error.message) : t('Failed to load')
      )
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    void loadEvents().catch((error: unknown) => {
      toast.error(
        error instanceof Error ? t(error.message) : t('Failed to load')
      )
    })
  }, [loadEvents, t])

  const groups = useMemo(
    () => groupNames(groupRatio, config),
    [config, groupRatio]
  )
  const dirty = useMemo(
    () => JSON.stringify(config) !== JSON.stringify(savedConfig),
    [config, savedConfig]
  )
  const patchConfig = (patch: Partial<PromptAuditConfig>) => {
    setConfig((current) => (current ? { ...current, ...patch } : current))
  }

  const patchEventFilter = (
    key: keyof PromptAuditFilterDraft,
    value: string
  ) => {
    setEventPage(1)
    setEventFilter((current) => ({ ...current, [key]: value }))
  }

  const patchEndpoint = (
    index: number,
    patch: Partial<PromptAuditEndpoint>
  ) => {
    if (!config) return
    const endpoints = config.endpoints.map((endpoint, itemIndex) =>
      itemIndex === index ? { ...endpoint, ...patch } : endpoint
    )
    patchConfig({ endpoints })
  }

  const toggleScanner = (scanner: string) => {
    if (!config) return
    const selected = new Set(config.scanners)
    if (selected.has(scanner)) selected.delete(scanner)
    else selected.add(scanner)
    patchConfig({
      scanners: scanners.map(([id]) => id).filter((id) => selected.has(id)),
    })
  }

  const save = async () => {
    if (!config) return
    const limitError = validatePromptAuditResourceLimits(config)
    if (limitError?.field === 'worker_count') {
      toast.error(
        t('Worker count must be an integer between {{min}} and {{max}}.', {
          min: promptAuditWorkerCountRange.min,
          max: promptAuditWorkerCountRange.max,
        })
      )
      return
    }
    if (limitError?.field === 'endpoint_input_limit') {
      const endpoint = config.endpoints[limitError.endpointIndex]
      toast.error(
        t(
          'Chunk size for endpoint {{endpoint}} must be an integer between {{min}} and {{max}}.',
          {
            endpoint:
              endpoint.name.trim() ||
              endpoint.id.trim() ||
              String(limitError.endpointIndex + 1),
            min: promptAuditEndpointInputLimitRange.min,
            max: promptAuditEndpointInputLimitRange.max,
          }
        )
      )
      return
    }
    setSaving(true)
    try {
      const saved = await updatePromptAuditConfig({
        expected_config_version: config.config_version,
        mode: config.mode,
        blocking_latest_turn_only: config.blocking_latest_turn_only,
        store_pass_events: config.store_pass_events,
        store_blocked_events_only: config.store_blocked_events_only,
        strategy: 'priority',
        worker_count: config.worker_count,
        queue_capacity: config.queue_capacity,
        scanners: config.scanners,
        fail_closed: true,
        endpoints: config.endpoints,
        group_policies: config.group_policies,
      })
      setConfig(cloneConfig(saved))
      setSavedConfig(cloneConfig(saved))
      setRuntime(await getPromptAuditRuntime())
      toast.success(t('Saved'))
    } catch (error) {
      toast.error(error instanceof Error ? t(error.message) : t('Save failed'))
    } finally {
      setSaving(false)
    }
  }

  const probe = async (endpoint: PromptAuditEndpoint) => {
    setProbing((current) => [...current, endpoint.id])
    try {
      const result = await probePromptAuditEndpoint(endpoint)
      if (result.ok) {
        toast.success(`${t(result.message)} (${result.latency_ms} ms)`)
      } else {
        toast.error(`${t(result.message)} (${result.latency_ms} ms)`)
      }
      setRuntime(await getPromptAuditRuntime())
    } finally {
      setProbing((current) => current.filter((id) => id !== endpoint.id))
    }
  }

  const addGroupPolicy = (group: string) => {
    if (!config || !group) return
    patchConfig({
      group_policies: {
        ...config.group_policies,
        [group]: {
          mode: config.mode,
          enabled: config.mode !== 'off',
          fail_closed: true,
          scanners: [...config.scanners],
        },
      },
    })
  }

  const openEvent = async (id: number) => {
    try {
      setActiveEvent(await getPromptAuditEvent(id))
    } catch (error) {
      toast.error(
        error instanceof Error ? t(error.message) : t('Failed to load')
      )
    }
  }

  const removeEvent = async (id: number) => {
    if (!window.confirm(t('Delete this audit event?'))) return
    await deletePromptAuditEvent(id)
    if (activeEvent?.id === id) setActiveEvent(null)
    await loadEvents()
  }

  const removeSelectedEvents = async () => {
    if (
      selectedEvents.length === 0 ||
      !window.confirm(t('Delete selected audit events?'))
    ) {
      return
    }
    setDeleting(true)
    try {
      await batchDeletePromptAuditEvents(selectedEvents)
      setSelectedEvents([])
      await loadEvents()
    } finally {
      setDeleting(false)
    }
  }

  const buildDeleteFilter = (): PromptAuditEventFilter | null => {
    const start = Math.floor(new Date(deleteStart).getTime() / 1000)
    const end = Math.floor(new Date(deleteEnd).getTime() / 1000)
    if (!Number.isFinite(start) || !Number.isFinite(end) || start >= end) {
      return null
    }
    return {
      ...eventFilterFromDraft(eventFilter),
      start_time: start,
      end_time: end,
    }
  }

  const runDeletePreview = async () => {
    const filter = buildDeleteFilter()
    if (!filter) {
      toast.error(t('Select a valid deletion time range'))
      return
    }
    setDeleting(true)
    try {
      setDeleteFilter(filter)
      setDeletePreview(await previewPromptAuditEventDelete(filter))
    } finally {
      setDeleting(false)
    }
  }

  const confirmFilterDelete = async () => {
    if (!deleteFilter || !deletePreview || deletePreview.matched_count === 0) {
      return
    }
    setDeleting(true)
    try {
      const result = await deletePromptAuditEventsByFilter(
        deleteFilter,
        deletePreview
      )
      toast.success(
        t('{{count}} audit events deleted', { count: result.deleted_events })
      )
      setDeleteDialogOpen(false)
      setDeletePreview(null)
      await loadEvents()
    } finally {
      setDeleting(false)
    }
  }

  if (loading || !config) {
    return (
      <SettingsSection title={t('Prompt Audit')}>
        <div className='flex h-40 items-center justify-center'>
          <Loader2 className='size-5 animate-spin' />
        </div>
      </SettingsSection>
    )
  }

  return (
    <SettingsSection title={t('Prompt Audit')}>
      <Tabs defaultValue='configuration'>
        <div className='flex flex-wrap items-center justify-between gap-3 border-b pb-4'>
          <TabsList variant='line'>
            <TabsTrigger value='configuration'>
              {t('Configuration')}
            </TabsTrigger>
            <TabsTrigger value='events'>
              {t('Events')} ({eventTotal})
            </TabsTrigger>
          </TabsList>
          <div className='flex items-center gap-2'>
            <Badge
              variant={
                runtime?.process_status === 'degraded' ? 'warning' : 'outline'
              }
            >
              <Activity className='size-3' />
              {t(runtime?.process_status ?? 'unknown')}
            </Badge>
            <Button
              type='button'
              variant='outline'
              size='icon'
              onClick={() => void Promise.all([load(), loadEvents()])}
              title={t('Refresh')}
            >
              <RefreshCw className='size-4' />
            </Button>
          </div>
        </div>
        {config.content_moderation_active ? (
          <div className='border-warning/40 bg-warning/10 text-warning-foreground mt-4 flex items-center gap-2 border px-3 py-2 text-sm'>
            <ShieldCheck className='size-4 shrink-0' />
            {t(
              'Content moderation is enabled. Disable it before enabling prompt audit.'
            )}
          </div>
        ) : null}
        {runtime ? (
          <div className='grid grid-cols-2 gap-x-6 gap-y-3 border-b py-4 text-sm md:grid-cols-4 lg:grid-cols-6'>
            <RuntimeValue
              label={t('Database')}
              value={t(runtime.database_status)}
            />
            <RuntimeValue label='Redis' value={t(runtime.redis_status)} />
            <RuntimeValue
              label={t('Workers')}
              value={`${runtime.worker_active}/${runtime.worker_total}`}
            />
            <RuntimeValue
              label={t('Queue')}
              value={String(runtime.queue.active ?? 0)}
            />
            <RuntimeValue
              label={t('Blocked')}
              value={String(runtime.guard_metrics.blocked ?? 0)}
            />
            <RuntimeValue
              label={t('Failovers')}
              value={String(runtime.guard_metrics.failovers ?? 0)}
            />
            <RuntimeValue
              label={t('Dropped')}
              value={String(runtime.guard_metrics.dropped ?? 0)}
            />
            <RuntimeValue
              label={t('Average latency')}
              value={`${runtime.guard_metrics.latency_avg_ms ?? 0} ms`}
            />
            <RuntimeValue
              label='P95'
              value={`${runtime.guard_metrics.latency_p95_ms ?? 0} ms`}
            />
            <RuntimeValue
              label={t('Config version')}
              value={`${runtime.active_config_version}/${runtime.expected_config_version}`}
            />
            <RuntimeValue
              label={t('Config loaded')}
              value={
                runtime.config_loaded_at
                  ? new Date(runtime.config_loaded_at).toLocaleString()
                  : '-'
              }
            />
            <RuntimeValue
              label={t('Last error')}
              value={t(
                runtime.config_load_error ||
                  runtime.last_error_message ||
                  runtime.last_error_code ||
                  '-'
              )}
            />
          </div>
        ) : null}

        <TabsContent value='configuration' className='space-y-8 pt-5'>
          <section className='grid gap-5 border-b pb-6 md:grid-cols-2'>
            <label className='space-y-2 text-sm'>
              <span className='font-medium'>{t('Default mode')}</span>
              <select
                className='border-input bg-background h-9 w-full rounded-md border px-3'
                value={config.mode}
                onChange={(event) => {
                  const mode = event.target.value as PromptAuditMode
                  if (
                    mode === 'blocking' &&
                    config.mode !== 'blocking' &&
                    !window.confirm(t('Enable synchronous prompt blocking?'))
                  ) {
                    return
                  }
                  patchConfig({ mode })
                }}
              >
                {modeOptions.map((option) => (
                  <option
                    key={option.value}
                    value={option.value}
                    disabled={
                      config.content_moderation_active && option.value !== 'off'
                    }
                  >
                    {t(option.label)}
                  </option>
                ))}
              </select>
            </label>
            <div className='grid gap-3 sm:grid-cols-2'>
              <ToggleRow
                label={t('Latest turn only')}
                checked={config.blocking_latest_turn_only}
                onChange={(value) =>
                  patchConfig({ blocking_latest_turn_only: value })
                }
              />
              <ToggleRow
                label={t('Store pass events')}
                checked={config.store_pass_events}
                disabled={config.store_blocked_events_only}
                onChange={(value) =>
                  patchConfig({
                    store_pass_events: value,
                    ...(value ? { store_blocked_events_only: false } : {}),
                  })
                }
              />
              <ToggleRow
                label={t('Store blocked events only')}
                checked={config.store_blocked_events_only}
                onChange={(value) =>
                  patchConfig({
                    store_blocked_events_only: value,
                    ...(value ? { store_pass_events: false } : {}),
                  })
                }
              />
            </div>
            <label className='space-y-2 text-sm'>
              <span className='font-medium'>{t('Worker count')}</span>
              <Input
                type='number'
                min={promptAuditWorkerCountRange.min}
                max={promptAuditWorkerCountRange.max}
                value={config.worker_count}
                onChange={(event) =>
                  patchConfig({ worker_count: Number(event.target.value) })
                }
              />
            </label>
            <label className='space-y-2 text-sm'>
              <span className='font-medium'>{t('Queue capacity')}</span>
              <Input
                type='number'
                min={1}
                max={100000}
                value={config.queue_capacity}
                onChange={(event) =>
                  patchConfig({ queue_capacity: Number(event.target.value) })
                }
              />
            </label>
          </section>

          <section className='space-y-4 border-b pb-6'>
            <div className='flex items-center justify-between gap-3'>
              <h3 className='text-sm font-semibold'>{t('Guard endpoints')}</h3>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() =>
                  patchConfig({
                    endpoints: [
                      ...config.endpoints,
                      newEndpoint(config.endpoints.length + 1),
                    ],
                  })
                }
              >
                <Plus className='size-4' /> {t('Add endpoint')}
              </Button>
            </div>
            <div className='divide-y rounded-md border'>
              {config.endpoints.map((endpoint, index) => (
                <div key={endpoint.id} className='space-y-4 p-4'>
                  <div className='grid gap-3 md:grid-cols-[auto_1fr_1fr_auto] md:items-end'>
                    <Switch
                      checked={endpoint.enabled}
                      onCheckedChange={(value) =>
                        patchEndpoint(index, { enabled: value })
                      }
                    />
                    <Field label={t('Name')}>
                      <Input
                        value={endpoint.name}
                        onChange={(event) =>
                          patchEndpoint(index, { name: event.target.value })
                        }
                      />
                    </Field>
                    <Field label='ID'>
                      <Input
                        value={endpoint.id}
                        onChange={(event) =>
                          patchEndpoint(index, { id: event.target.value })
                        }
                      />
                    </Field>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      title={t('Delete')}
                      onClick={() =>
                        patchConfig({
                          endpoints: config.endpoints.filter(
                            (_, itemIndex) => itemIndex !== index
                          ),
                        })
                      }
                    >
                      <Trash2 className='size-4' />
                    </Button>
                  </div>
                  <div className='grid gap-3 md:grid-cols-2'>
                    <Field label={t('Base URL')}>
                      <Input
                        value={endpoint.base_url}
                        onChange={(event) =>
                          patchEndpoint(index, { base_url: event.target.value })
                        }
                      />
                    </Field>
                    <Field label={t('Model')}>
                      <Input
                        value={endpoint.model}
                        onChange={(event) =>
                          patchEndpoint(index, { model: event.target.value })
                        }
                      />
                    </Field>
                    <Field label={t('API token')}>
                      <PasswordInput
                        value={endpoint.token ?? ''}
                        placeholder={
                          endpoint.has_token
                            ? t('Leave empty to keep the existing token')
                            : ''
                        }
                        onChange={(event) =>
                          patchEndpoint(index, {
                            token: event.target.value,
                            clear_token: false,
                          })
                        }
                      />
                    </Field>
                    <div className='grid grid-cols-2 gap-3'>
                      <Field label={t('Timeout (ms)')}>
                        <Input
                          type='number'
                          min={100}
                          max={120000}
                          value={endpoint.timeout_ms}
                          onChange={(event) =>
                            patchEndpoint(index, {
                              timeout_ms: Number(event.target.value),
                            })
                          }
                        />
                      </Field>
                      <Field label={t('Chunk size')}>
                        <Input
                          type='number'
                          min={promptAuditEndpointInputLimitRange.min}
                          max={promptAuditEndpointInputLimitRange.max}
                          value={endpoint.input_limit}
                          onChange={(event) =>
                            patchEndpoint(index, {
                              input_limit: Number(event.target.value),
                            })
                          }
                        />
                      </Field>
                    </div>
                  </div>
                  <div className='flex flex-wrap items-center justify-between gap-2'>
                    <div className='flex items-center gap-2'>
                      <span className='text-muted-foreground text-xs'>
                        {t(endpoint.token_status)}
                      </span>
                      {endpoint.has_token && !endpoint.clear_token ? (
                        <Button
                          type='button'
                          variant='ghost'
                          size='sm'
                          onClick={() =>
                            patchEndpoint(index, {
                              token: '',
                              clear_token: true,
                              has_token: false,
                              token_status: 'missing',
                            })
                          }
                        >
                          <Trash2 className='size-3.5' /> {t('Clear token')}
                        </Button>
                      ) : null}
                    </div>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      disabled={probing.includes(endpoint.id)}
                      onClick={() => void probe(endpoint)}
                    >
                      {probing.includes(endpoint.id) ? (
                        <Loader2 className='size-4 animate-spin' />
                      ) : (
                        <ShieldCheck className='size-4' />
                      )}
                      {t('Test connection')}
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          </section>

          <ScannerGrid
            title={t('Risk categories')}
            selected={config.scanners}
            onToggle={toggleScanner}
          />

          <section className='space-y-4 border-t pt-6'>
            <div className='flex flex-wrap items-center justify-between gap-3'>
              <h3 className='text-sm font-semibold'>{t('Group policies')}</h3>
              <select
                className='border-input bg-background h-8 rounded-md border px-2 text-sm'
                value=''
                onChange={(event) => addGroupPolicy(event.target.value)}
              >
                <option value=''>{t('Add group policy')}</option>
                {groups
                  .filter((group) => !config.group_policies[group])
                  .map((group) => (
                    <option key={group} value={group}>
                      {group}
                    </option>
                  ))}
              </select>
            </div>
            <div className='divide-y rounded-md border'>
              {Object.entries(config.group_policies).map(([group, policy]) => (
                <div key={group} className='space-y-4 p-4'>
                  <div className='grid gap-3 md:grid-cols-[minmax(150px,1fr)_minmax(180px,1fr)_auto] md:items-center'>
                    <strong className='text-sm'>{group}</strong>
                    <select
                      className='border-input bg-background h-9 rounded-md border px-3 text-sm'
                      value={policy.mode}
                      onChange={(event) =>
                        patchConfig({
                          group_policies: {
                            ...config.group_policies,
                            [group]: {
                              ...policy,
                              mode: event.target.value as PromptAuditMode,
                              enabled: event.target.value !== 'off',
                            },
                          },
                        })
                      }
                    >
                      {modeOptions.map((option) => (
                        <option
                          key={option.value}
                          value={option.value}
                          disabled={
                            config.content_moderation_active &&
                            option.value !== 'off'
                          }
                        >
                          {t(option.label)}
                        </option>
                      ))}
                    </select>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      title={t('Delete')}
                      onClick={() => {
                        const policies = { ...config.group_policies }
                        delete policies[group]
                        patchConfig({ group_policies: policies })
                      }}
                    >
                      <Trash2 className='size-4' />
                    </Button>
                  </div>
                  <ScannerGrid
                    selected={policy.scanners}
                    onToggle={(scanner) => {
                      const selected = new Set(policy.scanners)
                      if (selected.has(scanner)) selected.delete(scanner)
                      else selected.add(scanner)
                      patchConfig({
                        group_policies: {
                          ...config.group_policies,
                          [group]: {
                            ...policy,
                            scanners: scanners
                              .map(([id]) => id)
                              .filter((id) => selected.has(id)),
                          },
                        },
                      })
                    }}
                  />
                </div>
              ))}
            </div>
          </section>

          <div className='flex justify-end gap-2 border-t pt-5'>
            <Button
              type='button'
              variant='outline'
              onClick={() => savedConfig && setConfig(cloneConfig(savedConfig))}
              disabled={saving || !dirty}
            >
              <RotateCcw className='size-4' /> {t('Reset')}
            </Button>
            <Button
              type='button'
              onClick={() => void save()}
              disabled={saving || !dirty}
            >
              {saving ? (
                <Loader2 className='size-4 animate-spin' />
              ) : (
                <Save className='size-4' />
              )}
              {t('Save')}
            </Button>
          </div>
        </TabsContent>

        <TabsContent value='events' className='space-y-4 pt-5'>
          <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
            <select
              className='border-input bg-background h-9 rounded-md border px-3 text-sm'
              value={eventFilter.decision}
              onChange={(event) =>
                patchEventFilter('decision', event.target.value)
              }
            >
              <option value=''>{t('All decisions')}</option>
              <option value='critical'>{t('Critical')}</option>
              <option value='flag'>{t('Flagged')}</option>
              <option value='pass'>{t('Passed')}</option>
            </select>
            <select
              className='border-input bg-background h-9 rounded-md border px-3 text-sm'
              value={eventFilter.risk_level}
              onChange={(event) =>
                patchEventFilter('risk_level', event.target.value)
              }
            >
              <option value=''>{t('All risk levels')}</option>
              <option value='critical'>{t('Critical')}</option>
              <option value='high'>{t('High')}</option>
              <option value='medium'>{t('Medium')}</option>
              <option value='low'>{t('Low')}</option>
            </select>
            <Input
              placeholder={t('Filter by group')}
              value={eventFilter.group}
              onChange={(event) =>
                patchEventFilter('group', event.target.value)
              }
            />
            <Input
              type='number'
              min={1}
              placeholder={t('User ID')}
              value={eventFilter.user_id}
              onChange={(event) =>
                patchEventFilter('user_id', event.target.value)
              }
            />
            <Input
              type='number'
              min={1}
              placeholder={t('Token ID')}
              value={eventFilter.token_id}
              onChange={(event) =>
                patchEventFilter('token_id', event.target.value)
              }
            />
            <Input
              placeholder={t('Model')}
              value={eventFilter.model}
              onChange={(event) =>
                patchEventFilter('model', event.target.value)
              }
            />
            <Input
              placeholder={t('Endpoint')}
              value={eventFilter.endpoint}
              onChange={(event) =>
                patchEventFilter('endpoint', event.target.value)
              }
            />
            <Input
              placeholder={t('Request ID')}
              value={eventFilter.request_id}
              onChange={(event) =>
                patchEventFilter('request_id', event.target.value)
              }
            />
            <Input
              placeholder={t('Prompt hash')}
              value={eventFilter.prompt_hash}
              onChange={(event) =>
                patchEventFilter('prompt_hash', event.target.value)
              }
            />
            <Input
              placeholder={t('Keyword')}
              value={eventFilter.keyword}
              onChange={(event) =>
                patchEventFilter('keyword', event.target.value)
              }
            />
            <Field label={t('Start time')}>
              <Input
                type='datetime-local'
                value={eventFilter.start_time}
                onChange={(event) =>
                  patchEventFilter('start_time', event.target.value)
                }
              />
            </Field>
            <Field label={t('End time')}>
              <Input
                type='datetime-local'
                value={eventFilter.end_time}
                onChange={(event) =>
                  patchEventFilter('end_time', event.target.value)
                }
              />
            </Field>
          </div>
          <div className='flex flex-wrap gap-2'>
            <Button
              type='button'
              variant='outline'
              onClick={() => void loadEvents()}
            >
              <RefreshCw className='size-4' /> {t('Refresh')}
            </Button>
            <Button
              type='button'
              variant='outline'
              disabled={selectedEvents.length === 0 || deleting}
              onClick={() => void removeSelectedEvents()}
            >
              <Trash2 className='size-4' /> {t('Delete selected')}
            </Button>
            <Button
              type='button'
              variant='outline'
              onClick={() => {
                setDeletePreview(null)
                setDeleteDialogOpen(true)
              }}
            >
              <Trash2 className='size-4' /> {t('Delete by filter')}
            </Button>
            <Button
              type='button'
              variant='ghost'
              onClick={() => {
                setEventPage(1)
                setEventFilter(emptyPromptAuditFilter)
              }}
            >
              <RotateCcw className='size-4' /> {t('Reset')}
            </Button>
          </div>
          <div className='overflow-x-auto rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className='w-10'>
                    <Checkbox
                      checked={
                        events.length > 0 &&
                        events.every((event) =>
                          selectedEvents.includes(event.id)
                        )
                      }
                      onCheckedChange={(checked) =>
                        setSelectedEvents(
                          checked ? events.map((event) => event.id) : []
                        )
                      }
                    />
                  </TableHead>
                  <TableHead>{t('Time')}</TableHead>
                  <TableHead>{t('Decision')}</TableHead>
                  <TableHead>{t('User')}</TableHead>
                  <TableHead>{t('Group')}</TableHead>
                  <TableHead>{t('Model')}</TableHead>
                  <TableHead>{t('Prompt')}</TableHead>
                  <TableHead className='w-24' />
                </TableRow>
              </TableHeader>
              <TableBody>
                {events.map((event) => (
                  <TableRow key={event.id}>
                    <TableCell>
                      <Checkbox
                        checked={selectedEvents.includes(event.id)}
                        onCheckedChange={(checked) =>
                          setSelectedEvents((current) =>
                            checked
                              ? [...new Set([...current, event.id])]
                              : current.filter((id) => id !== event.id)
                          )
                        }
                      />
                    </TableCell>
                    <TableCell className='whitespace-nowrap'>
                      {new Date(event.created_at * 1000).toLocaleString()}
                    </TableCell>
                    <TableCell>
                      <Badge variant={decisionBadgeVariant(event.decision)}>
                        {t(event.decision)}
                      </Badge>
                    </TableCell>
                    <TableCell>{event.username || event.user_id}</TableCell>
                    <TableCell>{event.group}</TableCell>
                    <TableCell className='max-w-44 truncate'>
                      {event.model}
                    </TableCell>
                    <TableCell className='max-w-72 truncate'>
                      {event.redacted_preview}
                    </TableCell>
                    <TableCell>
                      <div className='flex justify-end gap-1'>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon'
                          title={t('View')}
                          onClick={() => void openEvent(event.id)}
                        >
                          <Eye className='size-4' />
                        </Button>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon'
                          title={t('Delete')}
                          onClick={() => void removeEvent(event.id)}
                        >
                          <Trash2 className='size-4' />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <div className='flex items-center justify-between'>
            <span className='text-muted-foreground text-sm'>
              {eventTotal} {t('events')}
            </span>
            <div className='flex gap-2'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={eventPage <= 1}
                onClick={() => setEventPage((page) => page - 1)}
              >
                {t('Previous')}
              </Button>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={eventPage * 20 >= eventTotal}
                onClick={() => setEventPage((page) => page + 1)}
              >
                {t('Next')}
              </Button>
            </div>
          </div>
        </TabsContent>
      </Tabs>

      <Dialog
        open={activeEvent !== null}
        onOpenChange={(open) => !open && setActiveEvent(null)}
      >
        <DialogContent className='max-h-[85vh] max-w-3xl overflow-y-auto'>
          <DialogHeader>
            <DialogTitle>{t('Prompt audit event')}</DialogTitle>
            <DialogDescription>{activeEvent?.request_id}</DialogDescription>
          </DialogHeader>
          {activeEvent ? (
            <Tabs defaultValue='summary' className='text-sm'>
              <TabsList variant='line'>
                <TabsTrigger value='summary'>{t('Summary')}</TabsTrigger>
                <TabsTrigger value='risks'>{t('Risks')}</TabsTrigger>
                <TabsTrigger value='technical'>{t('Technical')}</TabsTrigger>
              </TabsList>
              <TabsContent value='summary' className='space-y-5 pt-4'>
                <dl className='grid gap-3 sm:grid-cols-2'>
                  <Detail
                    label={t('Decision')}
                    value={`${t(activeEvent.decision)} / ${t(activeEvent.risk_level)}`}
                  />
                  <Detail
                    label={t('Guard endpoint')}
                    value={activeEvent.guard_endpoint_id}
                  />
                  <Detail
                    label={t('User')}
                    value={`${activeEvent.username} (${activeEvent.user_id})`}
                  />
                  <Detail label={t('Email')} value={activeEvent.user_email} />
                  <Detail
                    label={t('API token')}
                    value={`${activeEvent.token_name} (${activeEvent.token_id})`}
                  />
                  <Detail label={t('Group')} value={activeEvent.group} />
                  <Detail label={t('Model')} value={activeEvent.model} />
                  <Detail
                    label={t('Latency')}
                    value={`${activeEvent.latency_ms} ms`}
                  />
                </dl>
                <div>
                  <h4 className='mb-2 font-medium'>{t('Full prompt')}</h4>
                  <pre className='bg-muted max-h-80 overflow-auto rounded-md p-4 text-xs whitespace-pre-wrap'>
                    {activeEvent.full_prompt}
                  </pre>
                </div>
              </TabsContent>
              <TabsContent value='risks' className='space-y-5 pt-4'>
                <div>
                  <h4 className='mb-2 font-medium'>{t('Categories')}</h4>
                  <div className='flex flex-wrap gap-2'>
                    {activeEvent.categories?.map((category) => (
                      <Badge key={category} variant='outline'>
                        {t(category)}
                      </Badge>
                    ))}
                  </div>
                </div>
                {activeEvent.issue_summaries?.length ? (
                  <div className='divide-y rounded-md border'>
                    {activeEvent.issue_summaries.map((issue) => (
                      <div
                        key={`${issue.code}-${issue.category}`}
                        className='grid gap-1 p-3 sm:grid-cols-[1fr_auto]'
                      >
                        <strong>{issue.title}</strong>
                        <Badge
                          variant={
                            issue.severity === 'critical'
                              ? 'destructive'
                              : 'warning'
                          }
                        >
                          {issue.severity_label ?? t(issue.severity)}
                        </Badge>
                        <span className='text-muted-foreground text-xs'>
                          {t(issue.description ?? issue.code)}
                        </span>
                        <span className='text-xs'>
                          {issue.action_label ?? issue.action} /{' '}
                          {Math.round(issue.score * 100)}%
                        </span>
                        <code className='text-muted-foreground text-xs break-all sm:col-span-2'>
                          {issue.code}
                        </code>
                      </div>
                    ))}
                  </div>
                ) : null}
                <pre className='bg-muted max-h-72 overflow-auto rounded-md p-4 text-xs whitespace-pre-wrap'>
                  {JSON.stringify(
                    {
                      safety: activeEvent.safety,
                      action: activeEvent.action,
                      categories: activeEvent.categories,
                      matched_scanners: activeEvent.matched_scanners,
                      unknown_categories: activeEvent.unknown_categories,
                      scanner_scores: activeEvent.scanner_scores,
                      scanner_evidence: activeEvent.scanner_evidence,
                    },
                    null,
                    2
                  )}
                </pre>
              </TabsContent>
              <TabsContent value='technical' className='pt-4'>
                <dl className='grid gap-3 sm:grid-cols-2'>
                  <Detail
                    label={t('Request ID')}
                    value={activeEvent.request_id}
                  />
                  <Detail
                    label={t('Prompt hash')}
                    value={activeEvent.prompt_hash}
                  />
                  <Detail label={t('Endpoint')} value={activeEvent.endpoint} />
                  <Detail label={t('Protocol')} value={activeEvent.protocol} />
                  <Detail label={t('Stage')} value={activeEvent.stage} />
                  <Detail
                    label={t('Messages')}
                    value={String(activeEvent.message_count)}
                  />
                  <Detail
                    label={t('Prompt length')}
                    value={String(activeEvent.prompt_length)}
                  />
                  <Detail
                    label={t('Chunks')}
                    value={String(activeEvent.chunk_total)}
                  />
                  <Detail
                    label={t('Config version')}
                    value={String(activeEvent.config_version)}
                  />
                  <Detail label={t('Policy')} value={activeEvent.policy_id} />
                  <Detail
                    label={t('Policy version')}
                    value={String(activeEvent.policy_version)}
                  />
                  <Detail
                    label={t('Scanner')}
                    value={`${activeEvent.scanner_backend} / ${activeEvent.scanner_version}`}
                  />
                </dl>
              </TabsContent>
            </Tabs>
          ) : null}
        </DialogContent>
      </Dialog>

      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <DialogContent className='max-w-lg'>
          <DialogHeader>
            <DialogTitle>{t('Delete audit events by filter')}</DialogTitle>
            <DialogDescription>
              {t('The current event filters will be applied.')}
            </DialogDescription>
          </DialogHeader>
          <div className='grid gap-4 sm:grid-cols-2'>
            <Field label={t('Start time')}>
              <Input
                type='datetime-local'
                value={deleteStart}
                onChange={(event) => {
                  setDeleteStart(event.target.value)
                  setDeletePreview(null)
                }}
              />
            </Field>
            <Field label={t('End time')}>
              <Input
                type='datetime-local'
                value={deleteEnd}
                onChange={(event) => {
                  setDeleteEnd(event.target.value)
                  setDeletePreview(null)
                }}
              />
            </Field>
          </div>
          {deletePreview ? (
            <div className='border-y py-4 text-sm'>
              <strong>
                {t('{{count}} matching events', {
                  count: deletePreview.matched_count,
                })}
              </strong>
              <div className='text-muted-foreground mt-1 text-xs break-all'>
                {deletePreview.filter_hash}
              </div>
            </div>
          ) : null}
          <div className='flex justify-end gap-2'>
            <Button
              type='button'
              variant='outline'
              disabled={deleting}
              onClick={() => void runDeletePreview()}
            >
              {t('Preview')}
            </Button>
            <Button
              type='button'
              variant='destructive'
              disabled={
                deleting || !deletePreview || deletePreview.matched_count === 0
              }
              onClick={() => void confirmFilterDelete()}
            >
              <Trash2 className='size-4' /> {t('Confirm delete')}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </SettingsSection>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className='space-y-1.5 text-sm'>
      <span className='font-medium'>{label}</span>
      {children}
    </label>
  )
}

function ToggleRow({
  label,
  checked,
  disabled,
  onChange,
}: {
  label: string
  checked: boolean
  disabled?: boolean
  onChange: (value: boolean) => void
}) {
  return (
    <label className='flex min-h-9 items-center justify-between gap-3 text-sm'>
      <span>{label}</span>
      <Switch
        checked={checked}
        disabled={disabled}
        onCheckedChange={onChange}
      />
    </label>
  )
}

function ScannerGrid({
  title,
  selected,
  onToggle,
}: {
  title?: string
  selected: string[]
  onToggle: (scanner: string) => void
}) {
  const { t } = useTranslation()
  return (
    <section className='space-y-3'>
      {title ? <h3 className='text-sm font-semibold'>{title}</h3> : null}
      <div className='grid gap-2 sm:grid-cols-2 lg:grid-cols-3'>
        {scanners.map(([id, label]) => (
          <label key={id} className='flex min-h-9 items-center gap-2 text-sm'>
            <Checkbox
              checked={selected.includes(id)}
              onCheckedChange={() => onToggle(id)}
            />
            <span>{t(label)}</span>
          </label>
        ))}
      </div>
    </section>
  )
}

function Detail({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className='text-muted-foreground text-xs'>{label}</dt>
      <dd className='mt-1 break-all'>{value || '-'}</dd>
    </div>
  )
}

function RuntimeValue({ label, value }: { label: string; value: string }) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground truncate text-xs'>{label}</div>
      <div className='mt-1 truncate font-medium'>{value}</div>
    </div>
  )
}
