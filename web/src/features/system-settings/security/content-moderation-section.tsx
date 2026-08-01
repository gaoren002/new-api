import {
  Activity,
  Ban,
  ImagePlus,
  KeyRound,
  Loader2,
  RefreshCw,
  RotateCcw,
  Save,
  ShieldCheck,
  Trash2,
  UserCheck,
} from 'lucide-react'
import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
  type ChangeEvent,
  type ClipboardEvent,
} from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import { Textarea } from '@/components/ui/textarea'

import {
  clearContentModerationHashes,
  deleteContentModerationHash,
  getContentModerationConfig,
  getContentModerationRuntime,
  listContentModerationLogs,
  testContentModerationKeys,
  unbanContentModerationUser,
  updateContentModerationConfig,
} from '../api'
import { SettingsSection } from '../components/settings-section'
import type {
  ContentModerationConfig,
  ContentModerationLog,
  ContentModerationMode,
  ContentModerationRuntime,
  ContentModerationTestResult,
} from '../types'

const categoryLabels: Record<string, string> = {
  harassment: 'Harassment',
  'harassment/threatening': 'Harassment threatening',
  hate: 'Hate',
  'hate/threatening': 'Hate threatening',
  illicit: 'Illicit',
  'illicit/violent': 'Illicit violent',
  'self-harm': 'Self-harm',
  'self-harm/intent': 'Self-harm intent',
  'self-harm/instructions': 'Self-harm instructions',
  sexual: 'Sexual',
  'sexual/minors': 'Sexual minors',
  violence: 'Violence',
  'violence/graphic': 'Graphic violence',
}

const modeOptions: Array<{ value: ContentModerationMode; label: string }> = [
  { value: 'off', label: 'Off' },
  { value: 'observe', label: 'Observe only' },
  { value: 'pre_block', label: 'Pre-block' },
]

type ContentModerationSectionProps = { groupRatio: string }

function cloneConfig(config: ContentModerationConfig) {
  const cloned = structuredClone(config)
  cloned.groups = cloned.groups ?? []
  cloned.blocked_keywords = cloned.blocked_keywords ?? []
  cloned.api_key_statuses = cloned.api_key_statuses ?? []
  cloned.model_filter = cloned.model_filter ?? { type: 'all', models: [] }
  cloned.model_filter.models = cloned.model_filter.models ?? []
  cloned.thresholds = cloned.thresholds ?? {}
  cloned.proxy_url = cloned.proxy_url ?? ''
  cloned.cyber_policy_exclude_from_ban_count =
    cloned.cyber_policy_exclude_from_ban_count ?? false
  cloned.cyber_session_block_enabled =
    cloned.cyber_session_block_enabled ?? false
  cloned.cyber_session_block_ttl_seconds =
    cloned.cyber_session_block_ttl_seconds ?? 3600
  return cloned
}

function parseLines(value: string) {
  return value
    .split(/\r?\n/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function groupNames(
  groupRatio: string,
  config: ContentModerationConfig | null
) {
  const groups = new Set(config?.groups ?? [])
  try {
    const parsed = JSON.parse(groupRatio || '{}') as Record<string, unknown>
    Object.keys(parsed).forEach((group) => groups.add(group))
  } catch {
    // Saved group scopes remain editable if GroupRatio is temporarily invalid.
  }
  return [...groups].sort((left, right) => left.localeCompare(right))
}

function RuntimeValue({ label, value }: { label: string; value: string }) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground text-xs'>{label}</div>
      <div className='truncate text-sm font-medium' title={value}>
        {value}
      </div>
    </div>
  )
}

export function ContentModerationSection({
  groupRatio,
}: ContentModerationSectionProps) {
  const { t } = useTranslation()
  const [config, setConfig] = useState<ContentModerationConfig | null>(null)
  const [savedConfig, setSavedConfig] =
    useState<ContentModerationConfig | null>(null)
  const [runtime, setRuntime] = useState<ContentModerationRuntime | null>(null)
  const [logs, setLogs] = useState<ContentModerationLog[]>([])
  const [logTotal, setLogTotal] = useState(0)
  const [logPage, setLogPage] = useState(1)
  const [resultFilter, setResultFilter] = useState('')
  const [groupFilter, setGroupFilter] = useState('')
  const [searchFilter, setSearchFilter] = useState('')
  const [newKeys, setNewKeys] = useState('')
  const [deleteKeyHashes, setDeleteKeyHashes] = useState<string[]>([])
  const [testPrompt, setTestPrompt] = useState('This is a benign test.')
  const [testImages, setTestImages] = useState('')
  const [uploadedTestImage, setUploadedTestImage] = useState('')
  const [testResult, setTestResult] =
    useState<ContentModerationTestResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)

  const loadLogs = useCallback(async () => {
    const page = await listContentModerationLogs({
      page: logPage,
      page_size: 20,
      result: resultFilter || undefined,
      group: groupFilter || undefined,
      search: searchFilter || undefined,
    })
    setLogs(page.items)
    setLogTotal(page.total)
  }, [groupFilter, logPage, resultFilter, searchFilter])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [nextConfig, nextRuntime] = await Promise.all([
        getContentModerationConfig(),
        getContentModerationRuntime(),
      ])
      setConfig(cloneConfig(nextConfig))
      setSavedConfig(cloneConfig(nextConfig))
      setRuntime(nextRuntime)
      setNewKeys('')
      setDeleteKeyHashes([])
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
    void loadLogs().catch((error: unknown) => {
      toast.error(
        error instanceof Error ? t(error.message) : t('Failed to load')
      )
    })
  }, [loadLogs, t])

  const groups = useMemo(
    () => groupNames(groupRatio, config),
    [config, groupRatio]
  )
  const dirty = useMemo(
    () =>
      JSON.stringify(config) !== JSON.stringify(savedConfig) ||
      parseLines(newKeys).length > 0 ||
      deleteKeyHashes.length > 0,
    [config, deleteKeyHashes, newKeys, savedConfig]
  )

  const patchConfig = (patch: Partial<ContentModerationConfig>) => {
    setConfig((current) => (current ? { ...current, ...patch } : current))
  }

  const save = async () => {
    if (!config) return
    setSaving(true)
    try {
      const {
        api_key_count: _apiKeyCount,
        api_key_statuses: _apiKeyStatuses,
        config_version: configVersion,
        updated_at: _updatedAt,
        updated_by: _updatedBy,
        encryption_key_configured: _encryptionKeyConfigured,
        prompt_audit_active: _promptAuditActive,
        ...editable
      } = config
      const saved = await updateContentModerationConfig({
        ...editable,
        expected_config_version: configVersion,
        api_keys: parseLines(newKeys),
        delete_api_key_hashes: deleteKeyHashes,
        clear_api_keys: false,
      })
      setConfig(cloneConfig(saved))
      setSavedConfig(cloneConfig(saved))
      setNewKeys('')
      setDeleteKeyHashes([])
      setRuntime(await getContentModerationRuntime())
      toast.success(t('Saved'))
    } catch (error) {
      toast.error(error instanceof Error ? t(error.message) : t('Save failed'))
    } finally {
      setSaving(false)
    }
  }

  const testKeys = async () => {
    if (!config) return
    setTesting(true)
    try {
      const result = await testContentModerationKeys({
        api_keys: parseLines(newKeys),
        base_url: config.base_url,
        model: config.model,
        proxy_url: config.proxy_url,
        timeout_ms: config.timeout_ms,
        prompt: testPrompt,
        images: uploadedTestImage
          ? [uploadedTestImage]
          : parseLines(testImages).slice(0, 1),
      })
      setTestResult(result)
      const passed = result.items.filter(
        (item) => item.status === 'ready'
      ).length
      if (passed > 0) {
        toast.success(t('{{count}} API keys passed', { count: passed }))
      } else {
        toast.error(t('No API key passed the test'))
      }
      setRuntime(await getContentModerationRuntime())
    } catch (error) {
      toast.error(error instanceof Error ? t(error.message) : t('Test failed'))
    } finally {
      setTesting(false)
    }
  }

  const readTestImage = (file?: File) => {
    if (!file) return
    if (!file.type.startsWith('image/')) {
      toast.error(t('Please select an image file'))
      return
    }
    if (file.size > 8 * 1024 * 1024) {
      toast.error(t('The test image must be 8 MB or smaller'))
      return
    }
    const reader = new FileReader()
    reader.addEventListener('load', () => {
      if (typeof reader.result === 'string') {
        setUploadedTestImage(reader.result)
        setTestResult(null)
      }
    })
    reader.addEventListener('error', () =>
      toast.error(t('Failed to read the test image'))
    )
    reader.readAsDataURL(file)
  }

  const onTestImageChange = (event: ChangeEvent<HTMLInputElement>) => {
    readTestImage(event.target.files?.[0])
    event.target.value = ''
  }

  const onTestImagePaste = (event: ClipboardEvent<HTMLDivElement>) => {
    const image = [...event.clipboardData.files].find((file) =>
      file.type.startsWith('image/')
    )
    if (image) {
      event.preventDefault()
      readTestImage(image)
    }
  }

  const toggleGroup = (group: string, checked: boolean) => {
    if (!config) return
    const selected = new Set(config.groups)
    if (checked) selected.add(group)
    else selected.delete(group)
    patchConfig({ groups: [...selected].sort() })
  }

  const unbanUser = async (userID: number) => {
    if (!window.confirm(t('Unban this user?'))) return
    await unbanContentModerationUser(userID)
    await loadLogs()
    toast.success(t('User unbanned'))
  }

  const removeHash = async (inputHash: string) => {
    if (!window.confirm(t('Delete this risk hash?'))) return
    await deleteContentModerationHash(inputHash)
    setRuntime(await getContentModerationRuntime())
    toast.success(t('Risk hash deleted'))
  }

  const clearHashes = async () => {
    if (!window.confirm(t('Clear all risk hashes?'))) return
    const result = await clearContentModerationHashes()
    setRuntime(await getContentModerationRuntime())
    toast.success(t('{{count}} risk hashes deleted', { count: result.deleted }))
  }

  if (loading || !config) {
    return (
      <SettingsSection title={t('Content Moderation')}>
        <div className='flex h-40 items-center justify-center'>
          <Loader2 className='size-5 animate-spin' />
        </div>
      </SettingsSection>
    )
  }

  const conflict = config.prompt_audit_active
  let modeDescription = t(
    'Off mode disables proactive moderation checks; upstream cyber_policy handling remains active while the module is enabled.'
  )
  if (config.mode === 'pre_block') {
    modeDescription = t(
      'Synchronously reviews the latest user input before every request and rejects hits immediately.'
    )
  } else if (config.mode === 'observe') {
    modeDescription = t(
      'Requests pass through while the latest user input is queued for asynchronous review.'
    )
  }

  return (
    <SettingsSection title={t('Content Moderation')}>
      <Tabs defaultValue='configuration'>
        <div className='flex flex-wrap items-center justify-between gap-3 border-b pb-4'>
          <TabsList variant='line'>
            <TabsTrigger value='configuration'>
              {t('Configuration')}
            </TabsTrigger>
            <TabsTrigger value='logs'>
              {t('Audit logs')} ({logTotal})
            </TabsTrigger>
          </TabsList>
          <div className='flex items-center gap-2'>
            <Badge variant={runtime?.errors ? 'warning' : 'outline'}>
              <Activity className='size-3' />
              {t(runtime?.enabled ? runtime.mode : 'off')}
            </Badge>
            <Button
              type='button'
              variant='outline'
              size='icon'
              title={t('Refresh')}
              onClick={() => void Promise.all([load(), loadLogs()])}
            >
              <RefreshCw className='size-4' />
            </Button>
          </div>
        </div>

        {conflict ? (
          <div className='border-warning/40 bg-warning/10 text-warning-foreground mt-4 flex items-center gap-2 border px-3 py-2 text-sm'>
            <Ban className='size-4 shrink-0' />
            {t(
              'Prompt audit is enabled. Disable it before enabling content moderation.'
            )}
          </div>
        ) : null}

        {runtime ? (
          <div className='space-y-2 border-b py-4'>
            <p className='text-muted-foreground text-xs'>
              {t(
                'The worker queue handles asynchronous audits and record writes; synchronous pre-block checks are shown separately.'
              )}
            </p>
            <div className='grid grid-cols-2 gap-x-6 gap-y-3 md:grid-cols-4 lg:grid-cols-6'>
              <RuntimeValue
                label={t('Workers')}
                value={`${runtime.active_workers}/${runtime.worker_count}`}
              />
              <RuntimeValue
                label={t('Queue')}
                value={`${runtime.queue_length}/${runtime.queue_size}`}
              />
              <RuntimeValue
                label={t('Synchronous key load')}
                value={`${runtime.pre_block_api_key_active}/${runtime.pre_block_api_key_available_count}`}
              />
              <RuntimeValue
                label={t('Synchronous calls')}
                value={String(runtime.pre_block_api_key_total_calls)}
              />
              <RuntimeValue
                label={t('Processed')}
                value={String(runtime.processed)}
              />
              <RuntimeValue
                label={t('Dropped')}
                value={String(runtime.dropped)}
              />
              <RuntimeValue
                label={t('Blocked')}
                value={String(runtime.pre_block_blocked)}
              />
              <RuntimeValue
                label={t('Average latency')}
                value={`${runtime.pre_block_avg_latency_ms} ms`}
              />
              <RuntimeValue
                label={t('Risk hashes')}
                value={String(runtime.flagged_hash_count)}
              />
              <RuntimeValue
                label={t('Config version')}
                value={String(runtime.config_version)}
              />
            </div>
          </div>
        ) : null}

        <TabsContent value='configuration' className='space-y-7 pt-5'>
          <section className='grid gap-5 border-b pb-6 md:grid-cols-2'>
            <ToggleRow
              label={t('Enable content moderation')}
              description={t(
                'When disabled, moderation checks, cyber_policy recording, and cyber session blocking do not run.'
              )}
              checked={config.enabled}
              disabled={conflict && !config.enabled}
              onChange={(enabled) => patchConfig({ enabled })}
            />
            <Field label={t('Audit mode')} description={modeDescription}>
              <select
                className='border-input bg-background h-9 w-full rounded-md border px-3 text-sm'
                value={config.mode}
                onChange={(event) =>
                  patchConfig({
                    mode: event.target.value as ContentModerationMode,
                  })
                }
              >
                {modeOptions.map((option) => (
                  <option
                    key={option.value}
                    value={option.value}
                    disabled={conflict && option.value !== 'off'}
                  >
                    {t(option.label)}
                  </option>
                ))}
              </select>
            </Field>
            <Field
              label={t('Base URL')}
              description={t(
                'OpenAI-compatible base URL used for moderation requests.'
              )}
            >
              <Input
                value={config.base_url}
                onChange={(event) =>
                  patchConfig({ base_url: event.target.value })
                }
              />
            </Field>
            <Field
              label={t('Proxy URL')}
              description={t(
                'Optional HTTP, HTTPS, SOCKS5, or SOCKS5H proxy used only for moderation requests.'
              )}
            >
              <Input
                value={config.proxy_url}
                placeholder='socks5h://127.0.0.1:1080'
                onChange={(event) =>
                  patchConfig({ proxy_url: event.target.value })
                }
              />
            </Field>
            <Field label={t('Moderation model')}>
              <Input
                value={config.model}
                onChange={(event) => patchConfig({ model: event.target.value })}
              />
            </Field>
            <Field label={t('Timeout (ms)')}>
              <Input
                type='number'
                min={100}
                max={30000}
                value={config.timeout_ms}
                onChange={(event) =>
                  patchConfig({ timeout_ms: Number(event.target.value) })
                }
              />
            </Field>
            <Field label={t('Retry count')}>
              <Input
                type='number'
                min={0}
                max={5}
                value={config.retry_count}
                onChange={(event) =>
                  patchConfig({ retry_count: Number(event.target.value) })
                }
              />
            </Field>
            <Field
              label={t('Sample rate (%)')}
              description={t(
                'Percentage of eligible requests sent to moderation; zero disables API sampling.'
              )}
            >
              <Input
                type='number'
                min={0}
                max={100}
                value={config.sample_rate}
                onChange={(event) =>
                  patchConfig({ sample_rate: Number(event.target.value) })
                }
              />
            </Field>
            <Field
              label={t('Worker count')}
              description={t(
                'Workers process asynchronous audits and record writes, not synchronous pre-block checks.'
              )}
            >
              <Input
                type='number'
                min={1}
                max={32}
                value={config.worker_count}
                onChange={(event) =>
                  patchConfig({ worker_count: Number(event.target.value) })
                }
              />
            </Field>
            <Field
              label={t('Queue capacity')}
              description={t(
                'Maximum buffered asynchronous audit and record tasks before new tasks are dropped.'
              )}
            >
              <Input
                type='number'
                min={1}
                max={100000}
                value={config.queue_size}
                onChange={(event) =>
                  patchConfig({ queue_size: Number(event.target.value) })
                }
              />
            </Field>
          </section>

          <section className='space-y-4 border-b pb-6'>
            <div className='flex flex-wrap items-center justify-between gap-3'>
              <h3 className='text-sm font-semibold'>{t('API keys')}</h3>
              <Badge variant='outline'>
                <KeyRound className='size-3' /> {config.api_key_count}
              </Badge>
            </div>
            <div className='divide-y rounded-md border'>
              {config.api_key_statuses.map((key) => (
                <label
                  key={key.key_hash}
                  className='flex items-center gap-3 px-3 py-2 text-sm'
                >
                  <Checkbox
                    checked={deleteKeyHashes.includes(key.key_hash)}
                    onCheckedChange={(checked) =>
                      setDeleteKeyHashes((current) =>
                        checked === true
                          ? [...new Set([...current, key.key_hash])]
                          : current.filter((item) => item !== key.key_hash)
                      )
                    }
                  />
                  <span className='font-mono'>{key.masked}</span>
                  <Badge
                    className='ml-auto'
                    variant={key.status === 'ready' ? 'outline' : 'warning'}
                  >
                    {t(key.status)}
                  </Badge>
                  <span className='text-muted-foreground w-16 text-right text-xs'>
                    {key.last_latency_ms || 0} ms
                  </span>
                  <span
                    className='text-muted-foreground w-24 text-right text-xs'
                    title={t('Active / total calls / errors')}
                  >
                    {key.active}/{key.total_calls}/{key.error_count}
                  </span>
                </label>
              ))}
              {config.api_key_statuses.length === 0 ? (
                <div className='text-muted-foreground px-3 py-4 text-sm'>
                  {t('No API keys configured')}
                </div>
              ) : null}
            </div>
            <Field
              label={t('Add API keys')}
              description={t(
                'Keys are appended and de-duplicated on save. Authentication failures and rate limits temporarily freeze a key.'
              )}
            >
              <Textarea
                value={newKeys}
                rows={3}
                onChange={(event) => setNewKeys(event.target.value)}
              />
            </Field>
            <div
              className='grid gap-3 md:grid-cols-2'
              onPaste={onTestImagePaste}
            >
              <Field label={t('Test prompt')}>
                <Input
                  value={testPrompt}
                  onChange={(event) => setTestPrompt(event.target.value)}
                />
              </Field>
              <Field label={t('Test image URLs')}>
                <Input
                  value={testImages}
                  onChange={(event) => setTestImages(event.target.value)}
                />
              </Field>
              <div className='flex flex-wrap items-center justify-between gap-2 md:col-span-2'>
                <div className='flex items-center gap-2'>
                  <Input
                    id='content-moderation-test-image'
                    type='file'
                    accept='image/*'
                    className='hidden'
                    onChange={onTestImageChange}
                  />
                  <label
                    htmlFor='content-moderation-test-image'
                    className='border-input bg-background hover:bg-accent inline-flex h-9 cursor-pointer items-center gap-2 rounded-md border px-3 text-sm font-medium'
                  >
                    <ImagePlus className='size-4' /> {t('Add test image')}
                  </label>
                  {uploadedTestImage ? (
                    <Button
                      type='button'
                      variant='ghost'
                      onClick={() => {
                        setUploadedTestImage('')
                        setTestResult(null)
                      }}
                    >
                      <Trash2 className='size-4' /> {t('Remove image')}
                    </Button>
                  ) : null}
                  <span className='text-muted-foreground text-xs'>
                    {t('One image, up to 8 MB; paste is supported.')}
                  </span>
                </div>
                <Button
                  type='button'
                  variant='outline'
                  disabled={testing}
                  onClick={() => void testKeys()}
                >
                  {testing ? (
                    <Loader2 className='size-4 animate-spin' />
                  ) : (
                    <ShieldCheck className='size-4' />
                  )}
                  {t('Test connection')}
                </Button>
              </div>
              {uploadedTestImage ? (
                <img
                  src={uploadedTestImage}
                  alt={t('Test image preview')}
                  className='max-h-40 max-w-full rounded border object-contain md:col-span-2'
                />
              ) : null}
            </div>
            {testResult?.audit_result ? (
              <div className='space-y-3 border-t pt-4'>
                <div className='flex flex-wrap items-center justify-between gap-2'>
                  <h4 className='text-sm font-semibold'>
                    {t('Audit test result')}
                  </h4>
                  <Badge
                    variant={
                      testResult.audit_result.flagged
                        ? 'destructive'
                        : 'outline'
                    }
                  >
                    {t(
                      testResult.audit_result.flagged
                        ? 'Threshold hit'
                        : 'Passed'
                    )}
                  </Badge>
                </div>
                <div className='grid gap-3 sm:grid-cols-3'>
                  <RuntimeValue
                    label={t('Highest category')}
                    value={t(
                      categoryLabels[
                        testResult.audit_result.highest_category
                      ] ??
                        testResult.audit_result.highest_category ??
                        '-'
                    )}
                  />
                  <RuntimeValue
                    label={t('Highest score')}
                    value={testResult.audit_result.highest_score.toFixed(4)}
                  />
                  <RuntimeValue
                    label={t('Composite score')}
                    value={testResult.audit_result.composite_score.toFixed(4)}
                  />
                </div>
                <div className='grid gap-2 sm:grid-cols-2 lg:grid-cols-3'>
                  {Object.entries(testResult.audit_result.category_scores)
                    .sort(([, left], [, right]) => right - left)
                    .map(([category, score]) => (
                      <div
                        key={category}
                        className='flex items-center justify-between gap-3 border-b py-1 text-xs'
                      >
                        <span>{t(categoryLabels[category] ?? category)}</span>
                        <span className='font-mono'>
                          {score.toFixed(4)} /{' '}
                          {(
                            testResult.audit_result?.thresholds[category] ?? 1
                          ).toFixed(4)}
                        </span>
                      </div>
                    ))}
                </div>
              </div>
            ) : null}
          </section>

          <section className='space-y-4 border-b pb-6'>
            <ToggleRow
              label={t('Apply to all groups')}
              description={t(
                'Turn this off to moderate only the explicitly selected groups below.'
              )}
              checked={config.all_groups}
              onChange={(all_groups) => patchConfig({ all_groups })}
            />
            {!config.all_groups ? (
              <div className='grid gap-2 sm:grid-cols-2 lg:grid-cols-3'>
                {groups.map((group) => (
                  <label
                    key={group}
                    className='flex items-center gap-2 rounded-md border px-3 py-2 text-sm'
                  >
                    <Checkbox
                      checked={config.groups.includes(group)}
                      onCheckedChange={(checked) =>
                        toggleGroup(group, checked === true)
                      }
                    />
                    <span className='truncate'>{group}</span>
                  </label>
                ))}
              </div>
            ) : null}
            <div className='grid gap-4 md:grid-cols-2'>
              <Field
                label={t('Model filter')}
                description={t(
                  'Matches the model name requested by the client before channel model mapping.'
                )}
              >
                <select
                  className='border-input bg-background h-9 w-full rounded-md border px-3 text-sm'
                  value={config.model_filter.type}
                  onChange={(event) =>
                    patchConfig({
                      model_filter: {
                        ...config.model_filter,
                        type: event.target.value as
                          | 'all'
                          | 'include'
                          | 'exclude',
                      },
                    })
                  }
                >
                  <option value='all'>{t('All models')}</option>
                  <option value='include'>{t('Include models')}</option>
                  <option value='exclude'>{t('Exclude models')}</option>
                </select>
              </Field>
              {config.model_filter.type !== 'all' ? (
                <Field label={t('Model names')}>
                  <Textarea
                    rows={3}
                    value={config.model_filter.models.join('\n')}
                    onChange={(event) =>
                      patchConfig({
                        model_filter: {
                          ...config.model_filter,
                          models: parseLines(event.target.value),
                        },
                      })
                    }
                  />
                </Field>
              ) : null}
            </div>
          </section>

          <section className='grid gap-5 border-b pb-6 md:grid-cols-2'>
            <Field
              label={t('Keyword mode')}
              description={t(
                'Keyword + API blocks keyword hits immediately and sends misses to the moderation API; the other modes run only their selected check.'
              )}
            >
              <select
                className='border-input bg-background h-9 w-full rounded-md border px-3 text-sm'
                value={config.keyword_blocking_mode}
                onChange={(event) =>
                  patchConfig({
                    keyword_blocking_mode: event.target.value as
                      | 'keyword_only'
                      | 'keyword_and_api'
                      | 'api_only',
                  })
                }
              >
                <option value='keyword_only'>{t('Keywords only')}</option>
                <option value='keyword_and_api'>{t('Keywords and API')}</option>
                <option value='api_only'>{t('API only')}</option>
              </select>
            </Field>
            <Field label={t('Blocked keywords')}>
              <Textarea
                rows={5}
                value={config.blocked_keywords.join('\n')}
                onChange={(event) =>
                  patchConfig({
                    blocked_keywords: parseLines(event.target.value),
                  })
                }
              />
            </Field>
            <ToggleRow
              label={t('Risk hash pre-check')}
              description={t(
                'Previously flagged input hashes are rejected before calling the moderation API; hash blocks do not increment ban counts.'
              )}
              checked={config.pre_hash_check_enabled}
              onChange={(pre_hash_check_enabled) =>
                patchConfig({ pre_hash_check_enabled })
              }
            />
            <ToggleRow
              label={t('Record non-hit requests')}
              description={t(
                'Stores redacted summaries for sampled requests that did not hit a threshold.'
              )}
              checked={config.record_non_hits}
              onChange={(record_non_hits) => patchConfig({ record_non_hits })}
            />
          </section>

          <section className='space-y-4 border-b pb-6'>
            <h3 className='text-sm font-semibold'>
              {t('Category thresholds')}
            </h3>
            <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
              {Object.entries(config.thresholds).map(
                ([category, threshold]) => (
                  <Field
                    key={category}
                    label={t(categoryLabels[category] ?? category)}
                  >
                    <Input
                      type='number'
                      min={0}
                      max={1}
                      step={0.01}
                      value={threshold}
                      onChange={(event) =>
                        patchConfig({
                          thresholds: {
                            ...config.thresholds,
                            [category]: Number(event.target.value),
                          },
                        })
                      }
                    />
                  </Field>
                )
              )}
            </div>
          </section>

          <section className='grid gap-5 border-b pb-6 md:grid-cols-2'>
            <Field label={t('Block status')}>
              <Input
                type='number'
                min={400}
                max={599}
                value={config.block_status}
                onChange={(event) =>
                  patchConfig({ block_status: Number(event.target.value) })
                }
              />
            </Field>
            <Field label={t('Block message')}>
              <Input
                value={config.block_message}
                onChange={(event) =>
                  patchConfig({ block_message: event.target.value })
                }
              />
            </Field>
            <ToggleRow
              label={t('Email on hit')}
              description={t(
                'Sends a notice for moderation hits; cyber_policy notices are always sent.'
              )}
              checked={config.email_on_hit}
              onChange={(email_on_hit) => patchConfig({ email_on_hit })}
            />
            <ToggleRow
              label={t('Automatic user ban')}
              description={t(
                'Disables non-administrator users and revokes active sessions after the configured number of violations in the rolling window.'
              )}
              checked={config.auto_ban_enabled}
              onChange={(auto_ban_enabled) => patchConfig({ auto_ban_enabled })}
            />
            <ToggleRow
              label={t('Exclude cyber_policy from ban count')}
              description={t(
                'Cyber policy events remain in logs and still send notices, but are excluded from current and historical automatic-ban counts.'
              )}
              checked={config.cyber_policy_exclude_from_ban_count}
              onChange={(cyber_policy_exclude_from_ban_count) =>
                patchConfig({ cyber_policy_exclude_from_ban_count })
              }
            />
            <ToggleRow
              label={t('Block cyber_policy sessions')}
              description={t(
                'After an upstream cyber_policy rejection, temporarily blocks only the same explicit session identifier. Redis failures fail open.'
              )}
              checked={config.cyber_session_block_enabled}
              onChange={(cyber_session_block_enabled) =>
                patchConfig({ cyber_session_block_enabled })
              }
            />
            {config.cyber_session_block_enabled ? (
              <Field
                label={t('Cyber session block TTL (seconds)')}
                description={t(
                  'Applies only when session_id, conversation_id, or prompt_cache_key is explicitly provided.'
                )}
              >
                <Input
                  type='number'
                  min={60}
                  max={604800}
                  value={config.cyber_session_block_ttl_seconds}
                  onChange={(event) =>
                    patchConfig({
                      cyber_session_block_ttl_seconds: Number(
                        event.target.value
                      ),
                    })
                  }
                />
              </Field>
            ) : null}
            <Field label={t('Ban threshold')}>
              <Input
                type='number'
                min={1}
                value={config.ban_threshold}
                onChange={(event) =>
                  patchConfig({ ban_threshold: Number(event.target.value) })
                }
              />
            </Field>
            <Field label={t('Violation window (hours)')}>
              <Input
                type='number'
                min={1}
                value={config.violation_window_hours}
                onChange={(event) =>
                  patchConfig({
                    violation_window_hours: Number(event.target.value),
                  })
                }
              />
            </Field>
            <Field label={t('Hit retention (days)')}>
              <Input
                type='number'
                min={1}
                max={3650}
                value={config.hit_retention_days}
                onChange={(event) =>
                  patchConfig({
                    hit_retention_days: Number(event.target.value),
                  })
                }
              />
            </Field>
            <Field label={t('Non-hit retention (days)')}>
              <Input
                type='number'
                min={1}
                max={3}
                value={config.non_hit_retention_days}
                onChange={(event) =>
                  patchConfig({
                    non_hit_retention_days: Number(event.target.value),
                  })
                }
              />
            </Field>
          </section>

          <div className='flex justify-end gap-2'>
            <Button
              type='button'
              variant='outline'
              disabled={saving || !dirty}
              onClick={() => {
                if (savedConfig) setConfig(cloneConfig(savedConfig))
                setNewKeys('')
                setDeleteKeyHashes([])
              }}
            >
              <RotateCcw className='size-4' /> {t('Reset')}
            </Button>
            <Button
              type='button'
              disabled={saving || !dirty}
              onClick={() => void save()}
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

        <TabsContent value='logs' className='space-y-4 pt-5'>
          <div className='grid gap-3 md:grid-cols-[160px_160px_1fr_auto]'>
            <select
              className='border-input bg-background h-9 rounded-md border px-3 text-sm'
              value={resultFilter}
              onChange={(event) => {
                setResultFilter(event.target.value)
                setLogPage(1)
              }}
            >
              <option value=''>{t('All results')}</option>
              <option value='hit'>{t('Flagged')}</option>
              <option value='blocked'>{t('Blocked')}</option>
              <option value='pass'>{t('Passed')}</option>
              <option value='error'>{t('Error')}</option>
            </select>
            <select
              className='border-input bg-background h-9 rounded-md border px-3 text-sm'
              value={groupFilter}
              onChange={(event) => {
                setGroupFilter(event.target.value)
                setLogPage(1)
              }}
            >
              <option value=''>{t('All groups')}</option>
              {groups.map((group) => (
                <option key={group} value={group}>
                  {group}
                </option>
              ))}
            </select>
            <Input
              value={searchFilter}
              placeholder={t('Search logs')}
              onChange={(event) => {
                setSearchFilter(event.target.value)
                setLogPage(1)
              }}
            />
            <Button
              type='button'
              variant='outline'
              disabled={!runtime?.flagged_hash_count}
              onClick={() => void clearHashes()}
            >
              <Trash2 className='size-4' /> {t('Clear risk hashes')}
            </Button>
          </div>
          <div className='overflow-x-auto rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Time')}</TableHead>
                  <TableHead>{t('User')}</TableHead>
                  <TableHead>{t('Group')}</TableHead>
                  <TableHead>{t('Model')}</TableHead>
                  <TableHead>{t('Result')}</TableHead>
                  <TableHead>{t('Category')}</TableHead>
                  <TableHead>{t('Input preview')}</TableHead>
                  <TableHead className='text-right'>{t('Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {logs.map((log) => (
                  <TableRow key={log.id}>
                    <TableCell className='text-xs whitespace-nowrap'>
                      {new Date(log.created_at * 1000).toLocaleString()}
                    </TableCell>
                    <TableCell>
                      <div className='text-sm'>
                        {log.username || log.user_id}
                      </div>
                      <div className='text-muted-foreground text-xs'>
                        {log.user_email}
                      </div>
                    </TableCell>
                    <TableCell>{log.group}</TableCell>
                    <TableCell className='max-w-40 truncate' title={log.model}>
                      {log.model}
                    </TableCell>
                    <TableCell>
                      <Badge variant={log.flagged ? 'destructive' : 'outline'}>
                        {t(log.action)}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <div>
                        {t(
                          categoryLabels[log.highest_category] ??
                            log.highest_category ??
                            '-'
                        )}
                      </div>
                      <div className='text-muted-foreground text-xs'>
                        {log.highest_score.toFixed(4)}
                      </div>
                    </TableCell>
                    <TableCell
                      className='max-w-64 truncate text-xs'
                      title={log.input_excerpt}
                    >
                      {log.input_excerpt || '-'}
                    </TableCell>
                    <TableCell>
                      <div className='flex justify-end gap-1'>
                        {log.user_status !== 1 && log.user_id > 0 ? (
                          <Button
                            type='button'
                            variant='ghost'
                            size='icon'
                            title={t('Unban user')}
                            onClick={() => void unbanUser(log.user_id)}
                          >
                            <UserCheck className='size-4' />
                          </Button>
                        ) : null}
                        {log.flagged && log.input_hash ? (
                          <Button
                            type='button'
                            variant='ghost'
                            size='icon'
                            title={t('Delete risk hash')}
                            onClick={() => void removeHash(log.input_hash)}
                          >
                            <Trash2 className='size-4' />
                          </Button>
                        ) : null}
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
                {logs.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={8} className='h-24 text-center'>
                      {t('No audit logs')}
                    </TableCell>
                  </TableRow>
                ) : null}
              </TableBody>
            </Table>
          </div>
          <div className='flex items-center justify-between text-sm'>
            <span className='text-muted-foreground'>
              {t('{{count}} records', { count: logTotal })}
            </span>
            <div className='flex gap-2'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={logPage <= 1}
                onClick={() => setLogPage((page) => Math.max(1, page - 1))}
              >
                {t('Previous')}
              </Button>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={logPage * 20 >= logTotal}
                onClick={() => setLogPage((page) => page + 1)}
              >
                {t('Next')}
              </Button>
            </div>
          </div>
        </TabsContent>
      </Tabs>
    </SettingsSection>
  )
}

function Field({
  label,
  description,
  children,
}: {
  label: string
  description?: string
  children: ReactNode
}) {
  return (
    <label className='space-y-2 text-sm'>
      <span className='block font-medium'>{label}</span>
      {description ? (
        <span className='text-muted-foreground block text-xs leading-5'>
          {description}
        </span>
      ) : null}
      {children}
    </label>
  )
}

function ToggleRow({
  label,
  description,
  checked,
  disabled,
  onChange,
}: {
  label: string
  description?: string
  checked: boolean
  disabled?: boolean
  onChange: (checked: boolean) => void
}) {
  return (
    <label className='flex min-h-9 items-start justify-between gap-3 text-sm'>
      <span className='min-w-0'>
        <span className='block font-medium'>{label}</span>
        {description ? (
          <span className='text-muted-foreground mt-1 block text-xs leading-5'>
            {description}
          </span>
        ) : null}
      </span>
      <Switch
        className='mt-0.5 shrink-0'
        checked={checked}
        disabled={disabled}
        onCheckedChange={onChange}
      />
    </label>
  )
}
