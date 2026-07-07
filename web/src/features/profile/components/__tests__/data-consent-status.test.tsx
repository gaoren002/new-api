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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'

import type { UserProfile } from '../../types'
import { NotificationTab } from '../tabs/notification-tab'

let queryClient: QueryClient

beforeEach(() => {
  localStorage.clear()
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  queryClient.setQueryData(['status'], {
    data_consent_enabled: true,
    data_consent_agreement_version: 'v2',
    data_consent_agreement_content: 'Test data collection agreement',
  })
})

afterEach(() => {
  queryClient.clear()
  localStorage.clear()
})

function renderPreference(status: string, version: string): void {
  const profile: UserProfile = {
    id: 7,
    username: 'consent-user',
    display_name: 'Consent user',
    role: 1,
    group: 'default',
    quota: 100,
    used_quota: 0,
    request_count: 0,
    status: 1,
    aff_count: 0,
    aff_quota: 0,
    aff_history_quota: 0,
    created_time: 0,
    setting: JSON.stringify({
      notify_type: 'email',
      data_consent_status: status,
      data_consent_version: version,
      data_consent_updated_at: 1,
    }),
  }
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <NotificationTab profile={profile} onUpdate={() => undefined} />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

describe('profile data consent status', () => {
  test.each([
    ['accepted', 'Accepted'],
    ['declined', 'Rejected'],
  ])(
    'keeps the saved %s status when loading notification preferences',
    async (status, label) => {
      renderPreference(status, 'v2')

      expect(await screen.findByText(label)).toBeVisible()
      expect(screen.queryByText('Pending')).not.toBeInTheDocument()
    }
  )

  test('shows pending when the saved choice belongs to an older agreement', async () => {
    renderPreference('accepted', 'v1')

    expect(await screen.findByText('Pending')).toBeVisible()
    expect(screen.queryByText('Accepted')).not.toBeInTheDocument()
  })
})
