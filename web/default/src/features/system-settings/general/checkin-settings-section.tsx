/*
Copyright (C) 2023-2026 QuantumNous

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

For commercial licensing, please contact support@quantumnous.com
*/
import { useMemo } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type { CurrencyDisplayType } from '@/stores/system-config-store'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'

type CheckinTier = {
  min_used_cny: number
  min_cny: number
  max_cny: number
}

const DEFAULT_TIERS: CheckinTier[] = [
  { min_used_cny: 0, min_cny: 0, max_cny: 0.2 },
  { min_used_cny: 100, min_cny: 0, max_cny: 1 },
  { min_used_cny: 300, min_cny: 0, max_cny: 2 },
  { min_used_cny: 1000, min_cny: 0, max_cny: 5 },
]

const tierSchema = z
  .object({
    min_used_cny: z.coerce.number().min(0),
    min_cny: z.coerce.number().min(0),
    max_cny: z.coerce.number().min(0),
  })
  .refine((tier) => tier.max_cny >= tier.min_cny, {
    message: 'Max reward must be greater than or equal to minimum reward',
    path: ['max_cny'],
  })

const schema = z.object({
  enabled: z.boolean(),
  minQuota: z.coerce.number().int().min(0),
  maxQuota: z.coerce.number().int().min(0),
  tiered: z.boolean(),
  tiers: z.array(tierSchema),
  fallbackMode: z.enum(['legacy', 'none']),
})

type Values = z.infer<typeof schema>

function parseTiers(raw: string | undefined): CheckinTier[] {
  if (!raw || raw.trim() === '') return DEFAULT_TIERS
  try {
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return DEFAULT_TIERS
    return parsed
      .map((item) => ({
        min_used_cny: Number(item?.min_used_cny ?? 0),
        min_cny: Number(item?.min_cny ?? 0),
        max_cny: Number(item?.max_cny ?? 0),
      }))
      .filter(
        (item) =>
          Number.isFinite(item.min_used_cny) &&
          Number.isFinite(item.min_cny) &&
          Number.isFinite(item.max_cny)
      )
      .sort((a, b) => a.min_used_cny - b.min_used_cny)
  } catch {
    return DEFAULT_TIERS
  }
}

function normalizeTiers(tiers: CheckinTier[]): CheckinTier[] {
  return tiers
    .map((tier) => ({
      min_used_cny: Math.max(0, Math.round(Number(tier.min_used_cny) || 0)),
      min_cny: Math.max(0, Number(tier.min_cny) || 0),
      max_cny: Math.max(0, Number(tier.max_cny) || 0),
    }))
    .map((tier) =>
      tier.max_cny >= tier.min_cny
        ? tier
        : { ...tier, min_cny: tier.max_cny, max_cny: tier.min_cny }
    )
    .sort((a, b) => a.min_used_cny - b.min_used_cny)
}

function quotaToUSD(quota: number, quotaPerUnit: number) {
  const unit = quotaPerUnit > 0 ? quotaPerUnit : 500000
  return quota / unit
}

function parseDisplayType(value: string | undefined): CurrencyDisplayType {
  switch (value) {
    case 'CNY':
    case 'TOKENS':
    case 'CUSTOM':
    case 'USD':
      return value
    default:
      return 'USD'
  }
}

function getDisplayUnitLabel(
  displayType: CurrencyDisplayType,
  customSymbol: string
) {
  switch (displayType) {
    case 'CNY':
      return 'CNY'
    case 'TOKENS':
      return '额度'
    case 'CUSTOM':
      return customSymbol?.trim() || '自定义货币'
    case 'USD':
    default:
      return 'USD'
  }
}

function getDisplayExchangeRate(
  displayType: CurrencyDisplayType,
  usdExchangeRate: number,
  customExchangeRate: number
) {
  switch (displayType) {
    case 'CNY':
      return usdExchangeRate > 0 ? usdExchangeRate : 7.3
    case 'CUSTOM':
      return customExchangeRate > 0 ? customExchangeRate : 1
    case 'USD':
    default:
      return 1
  }
}

function quotaToDisplayAmount(
  quota: number,
  options: {
    quotaPerUnit: number
    usdExchangeRate: number
    quotaDisplayType: CurrencyDisplayType
    customCurrencyExchangeRate: number
  }
) {
  if (options.quotaDisplayType === 'TOKENS') return quota
  const amountUSD = quotaToUSD(quota, options.quotaPerUnit)
  return (
    amountUSD *
    getDisplayExchangeRate(
      options.quotaDisplayType,
      options.usdExchangeRate,
      options.customCurrencyExchangeRate
    )
  )
}

function formatDisplayAmount(
  amount: number,
  displayType: CurrencyDisplayType,
  customSymbol: string
) {
  if (displayType === 'TOKENS') {
    return new Intl.NumberFormat(undefined, {
      maximumFractionDigits: 0,
    }).format(amount)
  }
  if (displayType === 'CUSTOM') {
    const formatted = new Intl.NumberFormat(undefined, {
      minimumFractionDigits: 0,
      maximumFractionDigits: amount >= 1 ? 2 : 4,
    }).format(amount)
    return `${customSymbol?.trim() || '¤'}${formatted}`
  }
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: displayType,
    currencyDisplay: 'narrowSymbol',
    minimumFractionDigits: 0,
    maximumFractionDigits: amount >= 1 ? 2 : 4,
  }).format(amount)
}

export function CheckinSettingsSection({
  defaultValues,
}: {
  defaultValues: {
    enabled: boolean
    minQuota: number
    maxQuota: number
    tiered: boolean
    tiers: string
    fallbackMode: 'legacy' | 'none'
    quotaPerUnit: number
    usdExchangeRate: number
    quotaDisplayType: string
    customCurrencySymbol: string
    customCurrencyExchangeRate: number
  }
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const formDefaults = useMemo<Values>(
    () => ({
      enabled: defaultValues.enabled,
      minQuota: defaultValues.minQuota,
      maxQuota: defaultValues.maxQuota,
      tiered: defaultValues.tiered,
      tiers: parseTiers(defaultValues.tiers),
      fallbackMode: defaultValues.fallbackMode || 'legacy',
    }),
    [defaultValues]
  )

  const form = useForm<Values>({
    resolver: zodResolver(schema) as unknown as Resolver<Values>,
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  const { isDirty, isSubmitting } = form.formState
  const enabled = form.watch('enabled')
  const tiered = form.watch('tiered')
  const tiers = form.watch('tiers') || []
  const displayType = parseDisplayType(defaultValues.quotaDisplayType)
  const displayUnitLabel = getDisplayUnitLabel(
    displayType,
    defaultValues.customCurrencySymbol
  )
  const amountInputStep = displayType === 'TOKENS' ? 1 : 0.01

  const addTier = () => {
    const lastTier = tiers[tiers.length - 1]
    form.setValue(
      'tiers',
      [
        ...tiers,
        {
          min_used_cny: lastTier ? lastTier.min_used_cny + 100 : 0,
          min_cny: 0,
          max_cny: lastTier ? lastTier.max_cny : 0.2,
        },
      ],
      { shouldDirty: true, shouldValidate: true }
    )
  }

  const removeTier = (index: number) => {
    form.setValue(
      'tiers',
      tiers.filter((_, currentIndex) => currentIndex !== index),
      { shouldDirty: true, shouldValidate: true }
    )
  }

  async function onSubmit(values: Values) {
    const normalizedTiers = normalizeTiers(values.tiers)
    const normalizedTierString = JSON.stringify(normalizedTiers)
    const defaultTierString = JSON.stringify(parseTiers(defaultValues.tiers))
    const updates: Array<{ key: string; value: string | boolean | number }> =
      []
    const normalizedDefaultFallbackMode = defaultValues.fallbackMode || 'legacy'

    if (values.enabled !== defaultValues.enabled) {
      updates.push({
        key: 'checkin_setting.enabled',
        value: values.enabled,
      })
    }

    if (values.minQuota !== defaultValues.minQuota) {
      updates.push({
        key: 'checkin_setting.min_quota',
        value: values.minQuota,
      })
    }

    if (values.maxQuota !== defaultValues.maxQuota) {
      updates.push({
        key: 'checkin_setting.max_quota',
        value: values.maxQuota,
      })
    }

    if (values.tiered !== defaultValues.tiered) {
      updates.push({
        key: 'checkin_setting.tiered',
        value: values.tiered,
      })
    }

    if (normalizedTierString !== defaultTierString) {
      updates.push({
        key: 'checkin_setting.tiers',
        value: normalizedTierString,
      })
    }

    if (values.fallbackMode !== normalizedDefaultFallbackMode) {
      updates.push({
        key: 'checkin_setting.fallback_mode',
        value: values.fallbackMode,
      })
    }

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }

    form.reset({ ...values, tiers: normalizedTiers })
  }

  const legacyMinAmount = quotaToDisplayAmount(form.watch('minQuota'), {
    quotaPerUnit: defaultValues.quotaPerUnit,
    usdExchangeRate: defaultValues.usdExchangeRate,
    quotaDisplayType: displayType,
    customCurrencyExchangeRate: defaultValues.customCurrencyExchangeRate,
  })
  const legacyMaxAmount = quotaToDisplayAmount(form.watch('maxQuota'), {
    quotaPerUnit: defaultValues.quotaPerUnit,
    usdExchangeRate: defaultValues.usdExchangeRate,
    quotaDisplayType: displayType,
    customCurrencyExchangeRate: defaultValues.customCurrencyExchangeRate,
  })

  return (
    <SettingsSection title={t('Check-in Settings')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending || isSubmitting}
            isSaveDisabled={!isDirty}
            saveLabel='Save check-in settings'
          />
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable check-in feature')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Allow users to check in daily for random quota rewards'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={updateOption.isPending || isSubmitting}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          {enabled && (
            <>
              <FormField
                control={form.control}
                name='tiered'
                render={({ field }) => (
                  <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                    <div className='space-y-0.5'>
                      <FormLabel className='text-base'>
                        {t('启用累计使用分层签到')}
                      </FormLabel>
                      <FormDescription>
                        {t(
                          '按用户累计已使用额度匹配分层，再随机发放当前显示单位对应额度。当前单位：{{unit}}',
                          { unit: displayUnitLabel }
                        )}
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                        disabled={updateOption.isPending || isSubmitting}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />

              {!tiered && (
                <div className='grid gap-6 sm:grid-cols-2'>
                  <FormField
                    control={form.control}
                    name='minQuota'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Minimum check-in quota')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={0}
                            placeholder={t('1000')}
                            {...field}
                          />
                        </FormControl>
                        <FormDescription>
                          {formatDisplayAmount(
                            legacyMinAmount,
                            displayType,
                            defaultValues.customCurrencySymbol
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='maxQuota'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Maximum check-in quota')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={0}
                            placeholder={t('10000')}
                            {...field}
                          />
                        </FormControl>
                        <FormDescription>
                          {formatDisplayAmount(
                            legacyMaxAmount,
                            displayType,
                            defaultValues.customCurrencySymbol
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>
              )}

              {tiered && (
                <div className='space-y-4'>
                  <div className='rounded-lg border'>
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead>
                            {t('累计使用门槛')} ({displayUnitLabel})
                          </TableHead>
                          <TableHead>
                            {t('最小奖励')} ({displayUnitLabel})
                          </TableHead>
                          <TableHead>
                            {t('最大奖励')} ({displayUnitLabel})
                          </TableHead>
                          <TableHead className='w-16 text-right'>
                            {t('Actions')}
                          </TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {tiers.map((_, index) => (
                          <TableRow key={index}>
                            <TableCell>
                              <FormField
                                control={form.control}
                                name={`tiers.${index}.min_used_cny`}
                                render={({ field }) => (
                                  <FormItem>
                                    <FormControl>
                                      <Input
                                        type='number'
                                        min={0}
                                        step={1}
                                        value={field.value}
                                        onBlur={field.onBlur}
                                        name={field.name}
                                        ref={field.ref}
                                        onChange={(event) =>
                                          field.onChange(
                                            Number.parseInt(
                                              event.target.value
                                            ) || 0
                                          )
                                        }
                                      />
                                    </FormControl>
                                    <FormDescription>
                                      {formatDisplayAmount(
                                        Number(field.value) || 0,
                                        displayType,
                                        defaultValues.customCurrencySymbol
                                      )}
                                    </FormDescription>
                                    <FormMessage />
                                  </FormItem>
                                )}
                              />
                            </TableCell>
                            <TableCell>
                              <FormField
                                control={form.control}
                                name={`tiers.${index}.min_cny`}
                                render={({ field }) => (
                                  <FormItem>
                                    <FormControl>
                                      <Input
                                        type='number'
                                        min={0}
                                        step={amountInputStep}
                                        value={field.value}
                                        onBlur={field.onBlur}
                                        name={field.name}
                                        ref={field.ref}
                                        onChange={(event) =>
                                          field.onChange(
                                            Number.parseFloat(
                                              event.target.value
                                            ) || 0
                                          )
                                        }
                                      />
                                    </FormControl>
                                    <FormMessage />
                                  </FormItem>
                                )}
                              />
                            </TableCell>
                            <TableCell>
                              <FormField
                                control={form.control}
                                name={`tiers.${index}.max_cny`}
                                render={({ field }) => (
                                  <FormItem>
                                    <FormControl>
                                      <Input
                                        type='number'
                                        min={0}
                                        step={amountInputStep}
                                        value={field.value}
                                        onBlur={field.onBlur}
                                        name={field.name}
                                        ref={field.ref}
                                        onChange={(event) =>
                                          field.onChange(
                                            Number.parseFloat(
                                              event.target.value
                                            ) || 0
                                          )
                                        }
                                      />
                                    </FormControl>
                                    <FormDescription>
                                      {formatDisplayAmount(
                                        Number(field.value) || 0,
                                        displayType,
                                        defaultValues.customCurrencySymbol
                                      )}
                                    </FormDescription>
                                    <FormMessage />
                                  </FormItem>
                                )}
                              />
                            </TableCell>
                            <TableCell className='text-right'>
                              <Button
                                type='button'
                                variant='ghost'
                                size='icon-sm'
                                onClick={() => removeTier(index)}
                                disabled={tiers.length <= 1}
                              >
                                <Trash2 className='h-4 w-4' />
                              </Button>
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </div>

                  <div className='flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between'>
                    <Button type='button' variant='outline' onClick={addTier}>
                      <Plus className='mr-2 h-4 w-4' />
                      {t('Add tier')}
                    </Button>

                    <FormField
                      control={form.control}
                      name='fallbackMode'
                      render={({ field }) => (
                        <FormItem className='min-w-[240px]'>
                          <FormLabel>{t('无匹配分层时')}</FormLabel>
                          <Select
                            onValueChange={field.onChange}
                            value={field.value}
                          >
                            <FormControl>
                              <SelectTrigger>
                                <SelectValue />
                              </SelectTrigger>
                            </FormControl>
                            <SelectContent alignItemWithTrigger={false}>
                              <SelectGroup>
                                <SelectItem value='legacy'>
                                  {t('使用旧版额度范围')}
                                </SelectItem>
                                <SelectItem value='none'>
                                  {t('发放 0')}
                                </SelectItem>
                              </SelectGroup>
                            </SelectContent>
                          </Select>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </div>
                </div>
              )}
            </>
          )}
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
