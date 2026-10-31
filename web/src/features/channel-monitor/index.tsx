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
import { HeartPulse } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { SectionPageLayout } from '@/components/layout'

import { useChannelMonitorLink } from './hooks/use-channel-monitor-link'

export function ChannelMonitor() {
  const { t } = useTranslation()
  const link = useChannelMonitorLink()

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('Channel Monitor')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        {link ? (
          // The monitor keeps its own origin, which its API and storage need
          // (RelayPulse answers requests from an opaque origin with 403). The
          // sandbox only stops it from navigating the console away; a
          // same-origin page could lift it, but would be the site's own content.
          <iframe
            src={link}
            title={t('Channel Monitor')}
            className='h-full w-full rounded-lg border'
            // eslint-disable-next-line react/iframe-missing-sandbox -- see the comment above
            sandbox='allow-forms allow-popups allow-popups-to-escape-sandbox allow-same-origin allow-scripts'
          />
        ) : (
          <EmptyState
            icon={HeartPulse}
            title={t('Channel monitor is not configured')}
          />
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
