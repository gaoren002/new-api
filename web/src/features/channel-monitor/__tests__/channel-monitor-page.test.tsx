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
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { ChannelMonitor } from '..'

beforeEach(() => {
  vi.stubGlobal('localStorage', {
    getItem: () => null,
    setItem: () => undefined,
    removeItem: () => undefined,
  })
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function renderMonitor(status: Record<string, unknown>) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['status'], status)
  render(
    <QueryClientProvider client={client}>
      <ChannelMonitor />
    </QueryClientProvider>
  )
}

test('a configured link is embedded in a titled frame that may run scripts but not navigate the console', () => {
  renderMonitor({ monitor_link: ' https://status.example.com/p/demo?embed=1 ' })

  const frame = screen.getByTitle('Channel Monitor')
  expect(frame.tagName).toBe('IFRAME')
  expect(frame).toHaveAttribute(
    'src',
    'https://status.example.com/p/demo?embed=1'
  )
  const sandbox = frame.getAttribute('sandbox') ?? ''
  expect(sandbox).toContain('allow-scripts')
  expect(sandbox).not.toContain('allow-top-navigation')
})

test.each([{}, { monitor_link: '' }, { monitor_link: 'javascript:alert(1)' }])(
  'without a usable link (%j) the page says the monitor is not configured instead of framing it',
  (status) => {
    renderMonitor(status)

    expect(screen.queryByTitle('Channel Monitor')).not.toBeInTheDocument()
    expect(
      screen.getByText('Channel monitor is not configured')
    ).toBeInTheDocument()
  }
)
