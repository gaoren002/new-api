import { useState } from 'react'
import { Database, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { useStatus } from '@/hooks/use-status'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Markdown } from '@/components/ui/markdown'
import { updateDataConsent } from '../api'
import {
  getDataConsentSettingsFromUser,
  getDataConsentStatus,
  updateUserDataConsentSetting,
} from '../lib/data-consent'

export function DataConsentDialog() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const user = useAuthStore((state) => state.auth.user)
  const setUser = useAuthStore((state) => state.auth.setUser)
  const [submitting, setSubmitting] = useState<'accepted' | 'declined' | null>(
    null
  )

  const enabled = Boolean(status?.data_consent_enabled)
  const agreementVersion =
    (status?.data_consent_agreement_version as string | undefined) || 'v1'
  const acceptedMultiplier =
    Number(status?.data_consent_accepted_multiplier) || 0.95
  const declinedMultiplier =
    Number(status?.data_consent_declined_multiplier) || 1.0
  const agreementContent =
    (status?.data_consent_agreement_content as string | undefined) ||
    t(
      'Authorize SuperAPI to collect your conversation data to improve user experience and domestic model quality.'
    )
  const consentStatus = getDataConsentStatus(
    getDataConsentSettingsFromUser(user),
    agreementVersion
  )
  const open = enabled && Boolean(user) && consentStatus === 'unset'

  const handleChoice = async (choice: 'accepted' | 'declined') => {
    if (!user) return
    try {
      setSubmitting(choice)
      const response = await updateDataConsent({ status: choice })
      if (response.success && response.data) {
        setUser(
          updateUserDataConsentSetting(
            user,
            choice,
            response.data.version || agreementVersion,
            response.data.updated_at || Math.floor(Date.now() / 1000)
          )
        )
        toast.success(t('Data authorization preference saved'))
      } else {
        toast.error(response.message || t('Failed to update settings'))
      }
    } catch (_error) {
      toast.error(t('Failed to update settings'))
    } finally {
      setSubmitting(null)
    }
  }

  return (
    <Dialog open={open}>
      <DialogContent showCloseButton={false} className='sm:max-w-2xl'>
        <DialogHeader>
          <div className='bg-primary/10 text-primary mb-1 flex size-10 items-center justify-center rounded-lg'>
            <Database className='size-5' />
          </div>
          <DialogTitle>{t('Data Authorization Agreement')}</DialogTitle>
          <DialogDescription>
            {t('Please read and choose whether to authorize data collection.')}
          </DialogDescription>
        </DialogHeader>

        <div className='bg-muted/30 max-h-[42vh] overflow-y-auto rounded-lg border p-3'>
          <Markdown className='text-sm'>{agreementContent}</Markdown>
        </div>

        <div className='grid gap-2 text-sm'>
          <div className='rounded-lg border p-3'>
            <div className='font-medium'>{t('Accept authorization')}</div>
            <div className='text-muted-foreground mt-1 text-xs'>
              {t('Authorized requests use {{multiplier}}x pricing.', {
                multiplier: acceptedMultiplier,
              })}
            </div>
          </div>
          <div className='rounded-lg border p-3'>
            <div className='font-medium'>{t('Reject authorization')}</div>
            <div className='text-muted-foreground mt-1 text-xs'>
              {t(
                'No data will be collected and requests use {{multiplier}}x pricing.',
                {
                  multiplier: declinedMultiplier,
                }
              )}
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button
            variant='outline'
            onClick={() => handleChoice('declined')}
            disabled={submitting !== null}
          >
            {submitting === 'declined' && <Loader2 className='animate-spin' />}
            {t('Reject')}
          </Button>
          <Button
            onClick={() => handleChoice('accepted')}
            disabled={submitting !== null}
          >
            {submitting === 'accepted' && <Loader2 className='animate-spin' />}
            {t('Accept')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
