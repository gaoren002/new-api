import type { AuthUser } from '@/stores/auth-store'
import type { UserSettings } from '../types'
import { parseUserSettings } from './format'

export type DataConsentStatus = 'accepted' | 'declined' | 'unset'

export function getDataConsentSettingsFromUser(
  user: Pick<AuthUser, 'setting'> | null | undefined
): UserSettings {
  const setting = user?.setting
  if (!setting) return {}
  if (typeof setting === 'string') {
    return parseUserSettings(setting)
  }
  return setting as UserSettings
}

export function getDataConsentStatus(
  settings: UserSettings,
  agreementVersion?: string
): DataConsentStatus {
  const status = settings.data_consent_status
  if (
    agreementVersion &&
    settings.data_consent_version &&
    settings.data_consent_version !== agreementVersion
  ) {
    return 'unset'
  }
  if (status === 'accepted') return 'accepted'
  if (status === 'declined') return 'declined'
  return 'unset'
}

export function updateUserDataConsentSetting<T extends AuthUser>(
  user: T,
  status: 'accepted' | 'declined',
  version: string,
  updatedAt: number
): T {
  const currentSettings = getDataConsentSettingsFromUser(user)
  const nextSettings = {
    ...currentSettings,
    data_consent_status: status,
    data_consent_version: version,
    data_consent_updated_at: updatedAt,
  }

  return {
    ...user,
    setting:
      typeof user.setting === 'string' ? JSON.stringify(nextSettings) : nextSettings,
  }
}
