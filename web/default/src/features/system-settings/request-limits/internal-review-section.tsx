/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useRef } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { PasswordInput } from '@/components/password-input'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const createInternalReviewSchema = (t: (key: string) => string) =>
  z.object({
    'internal_review.enabled': z.boolean(),
    'internal_review.endpoint': z
      .string()
      .trim()
      .refine(
        (value) => {
          if (!value) return true
          try {
            const url = new URL(value)
            return url.protocol === 'http:' || url.protocol === 'https:'
          } catch {
            return false
          }
        },
        { message: t('Enter a valid HTTP or HTTPS URL') }
      ),
    'internal_review.bearer_token': z.string(),
    'internal_review.timeout_seconds': z.number().min(1).max(120),
    'internal_review.fail_closed': z.boolean(),
    scope_text: z.boolean(),
    scope_image: z.boolean(),
    'internal_review.model_filter': z.string(),
  })

type InternalReviewFormValues = z.infer<
  ReturnType<typeof createInternalReviewSchema>
>

type InternalReviewValues = {
  'internal_review.enabled': boolean
  'internal_review.endpoint': string
  'internal_review.bearer_token': string
  'internal_review.timeout_seconds': number
  'internal_review.fail_closed': boolean
  'internal_review.scope': string
  'internal_review.model_filter': string
}

type InternalReviewSectionProps = {
  defaultValues: InternalReviewValues
}

const scopeToChecks = (scope: string) => {
  const scopes = scope
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
  const all = scopes.includes('all')
  return {
    scope_text: all || scopes.includes('text'),
    scope_image: all || scopes.includes('image'),
  }
}

const buildFormDefaults = (
  defaults: InternalReviewValues
): InternalReviewFormValues => ({
  'internal_review.enabled': defaults['internal_review.enabled'],
  'internal_review.endpoint': defaults['internal_review.endpoint'],
  'internal_review.bearer_token': defaults['internal_review.bearer_token'],
  'internal_review.timeout_seconds':
    defaults['internal_review.timeout_seconds'],
  'internal_review.fail_closed': defaults['internal_review.fail_closed'],
  ...scopeToChecks(defaults['internal_review.scope']),
  'internal_review.model_filter': defaults['internal_review.model_filter'],
})

const normalizeFormValues = (
  values: InternalReviewFormValues
): InternalReviewValues => {
  const scopes = [
    values.scope_text ? 'text' : '',
    values.scope_image ? 'image' : '',
  ].filter(Boolean)
  const modelFilter = values['internal_review.model_filter']
    .split(/[\n,]+/)
    .map((item) => item.trim())
    .filter(Boolean)
    .join(',')

  return {
    'internal_review.enabled': values['internal_review.enabled'],
    'internal_review.endpoint': values['internal_review.endpoint'].trim(),
    'internal_review.bearer_token':
      values['internal_review.bearer_token'].trim(),
    'internal_review.timeout_seconds':
      values['internal_review.timeout_seconds'],
    'internal_review.fail_closed': values['internal_review.fail_closed'],
    'internal_review.scope': scopes.join(','),
    'internal_review.model_filter': modelFilter,
  }
}

export function InternalReviewSection({
  defaultValues,
}: InternalReviewSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const schema = createInternalReviewSchema(t)
  const baselineRef = useRef<InternalReviewValues>(defaultValues)

  const form = useForm<InternalReviewFormValues>({
    resolver: zodResolver(schema),
    defaultValues: buildFormDefaults(defaultValues),
  })

  useEffect(() => {
    baselineRef.current = defaultValues
    form.reset(buildFormDefaults(defaultValues))
  }, [defaultValues, form])

  const onSubmit = async (values: InternalReviewFormValues) => {
    const normalized = normalizeFormValues(values)
    const updates = (
      Object.keys(normalized) as Array<keyof InternalReviewValues>
    ).filter((key) => normalized[key] !== baselineRef.current[key])

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const key of updates) {
      await updateOption.mutateAsync({ key, value: normalized[key] })
    }

    baselineRef.current = normalized
  }

  return (
    <SettingsSection
      title={t('Internal Review')}
      description={t(
        'Send requests to an internal review service before forwarding them upstream. Blocked requests keep the pre-consumed quota.'
      )}
    >
      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-6'>
          <div className='space-y-4'>
            <FormField
              control={form.control}
              name='internal_review.enabled'
              render={({ field }) => (
                <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                  <div className='space-y-0.5'>
                    <FormLabel className='text-base'>
                      {t('Enable internal review')}
                    </FormLabel>
                    <FormDescription>
                      {t(
                        'Requests are reviewed after pre-consumption and before upstream dispatch.'
                      )}
                    </FormDescription>
                  </div>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='internal_review.fail_closed'
              render={({ field }) => (
                <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                  <div className='space-y-0.5'>
                    <FormLabel className='text-base'>
                      {t('Block on review errors')}
                    </FormLabel>
                    <FormDescription>
                      {t(
                        'When enabled, review timeouts or service errors stop the request.'
                      )}
                    </FormDescription>
                  </div>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-4 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='internal_review.endpoint'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Review endpoint')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='https://review.example.com/review'
                      value={field.value}
                      onChange={field.onChange}
                      onBlur={field.onBlur}
                      name={field.name}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'NewAPI sends a POST request with model, text, request body and metadata.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='internal_review.bearer_token'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Bearer token')}</FormLabel>
                  <FormControl>
                    <PasswordInput
                      placeholder={t('Optional')}
                      autoComplete='new-password'
                      value={field.value}
                      onChange={field.onChange}
                      onBlur={field.onBlur}
                      name={field.name}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Sent as Authorization: Bearer token. Leave blank if unused.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-4 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='internal_review.timeout_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Timeout')}</FormLabel>
                  <FormControl>
                    <div className='flex items-center gap-2'>
                      <Input
                        type='number'
                        min={1}
                        max={120}
                        step={1}
                        value={field.value}
                        onBlur={field.onBlur}
                        name={field.name}
                        ref={field.ref}
                        onChange={(event) =>
                          field.onChange(
                            Number.parseInt(event.target.value) || 1
                          )
                        }
                      />
                      <span className='text-muted-foreground text-sm'>
                        {t('seconds')}
                      </span>
                    </div>
                  </FormControl>
                  <FormDescription>
                    {t('Maximum wait time for the internal review service.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <div className='space-y-3'>
              <FormLabel>{t('Review scope')}</FormLabel>
              <div className='grid gap-3 rounded-lg border p-4'>
                <FormField
                  control={form.control}
                  name='scope_text'
                  render={({ field }) => (
                    <FormItem className='flex flex-row items-start space-y-0 space-x-3'>
                      <FormControl>
                        <Checkbox
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                      <div className='space-y-1 leading-none'>
                        <FormLabel>{t('Text requests')}</FormLabel>
                        <FormDescription>
                          {t(
                            'Chat, responses, Claude and Gemini text requests.'
                          )}
                        </FormDescription>
                      </div>
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='scope_image'
                  render={({ field }) => (
                    <FormItem className='flex flex-row items-start space-y-0 space-x-3'>
                      <FormControl>
                        <Checkbox
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                      <div className='space-y-1 leading-none'>
                        <FormLabel>{t('Image requests')}</FormLabel>
                        <FormDescription>
                          {t('Image generation and edit prompts.')}
                        </FormDescription>
                      </div>
                    </FormItem>
                  )}
                />
              </div>
            </div>
          </div>

          <FormField
            control={form.control}
            name='internal_review.model_filter'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Model filter')}</FormLabel>
                <FormControl>
                  <Textarea
                    rows={4}
                    placeholder='gpt-4o,claude,!test-model'
                    value={field.value}
                    onChange={field.onChange}
                    onBlur={field.onBlur}
                    name={field.name}
                    ref={field.ref}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Optional comma-separated model keywords. Prefix with ! to exclude. Empty applies to all models.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <Button type='submit' disabled={updateOption.isPending}>
            {updateOption.isPending
              ? t('Saving...')
              : t('Save internal review')}
          </Button>
        </form>
      </Form>
    </SettingsSection>
  )
}
