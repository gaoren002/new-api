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
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { SystemInfoSection } from '../system-info-section'

function Fixture() {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setContainer} />
      <SettingsPageProvider actionsContainer={container}>
        <SystemInfoSection
          defaultValues={{
            SystemName: 'New API',
            ServerAddress: '',
            TaskPublicAddress: '',
            Logo: '',
            Footer: '',
            About: '',
            HomePageContent: '',
            general_setting: { docs_link: '', monitor_link: '' },
            legal: { user_agreement: '', privacy_policy: '' },
          }}
        />
      </SettingsPageProvider>
    </>
  )
}

async function renderSettings() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const router = createRouter({
    routeTree: createRootRoute({ component: Fixture }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return screen.findByRole('textbox', { name: 'Channel Monitor Link' })
}

beforeEach(() => {
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
})

test('saving a channel monitor link stores the address without surrounding spaces', async () => {
  const input = await renderSettings()

  fireEvent.change(input, {
    target: { value: '  https://status.example.com/p/demo?embed=1  ' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))

  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith('/api/option/', {
      key: 'general_setting.monitor_link',
      value: 'https://status.example.com/p/demo?embed=1',
    })
  )
  expect(api.put).toHaveBeenCalledTimes(1)
})

test.each(['status.example.com', 'javascript:alert(1)'])(
  'a channel monitor link %j that is not an http(s) address shows a field error and is not saved',
  async (value) => {
    const input = await renderSettings()

    fireEvent.change(input, { target: { value } })
    fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() => expect(input).toHaveAttribute('aria-invalid', 'true'))
    expect(
      screen.getByText('Provide a valid URL starting with http:// or https://')
    ).toBeInTheDocument()
    expect(api.put).not.toHaveBeenCalled()
  }
)
