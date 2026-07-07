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
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test } from 'vitest'

import { CheckinSettingsSection } from '../checkin-settings-section'

let queryClient: QueryClient

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
})

afterEach(() => queryClient.clear())

function renderTiers(): void {
  render(
    <QueryClientProvider client={queryClient}>
      <CheckinSettingsSection
        defaultValues={{
          enabled: true,
          minQuota: 0,
          maxQuota: 100,
          tiered: true,
          tiers: JSON.stringify([
            { min_used_cny: 0, min_cny: 0, max_cny: 0.2 },
            { min_used_cny: 100, min_cny: 0, max_cny: 1 },
            { min_used_cny: 300, min_cny: 0, max_cny: 2 },
          ]),
          fallbackMode: 'legacy',
          quotaPerUnit: 500000,
          usdExchangeRate: 7,
          quotaDisplayType: 'CNY',
          customCurrencySymbol: '',
          customCurrencyExchangeRate: 1,
        }}
      />
    </QueryClientProvider>
  )
}

describe('check-in tier editing', () => {
  test('removing a preceding tier preserves the focused remaining input and its value', () => {
    renderTiers()
    const rows = screen.getAllByRole('row')
    const threshold = within(rows[2]).getAllByRole('spinbutton')[0]
    act(() => threshold.focus())

    fireEvent.click(within(rows[1]).getByRole('button', { name: 'Delete' }))

    expect(threshold).toHaveFocus()
    expect(threshold).toHaveValue(100)
    expect(screen.getAllByRole('row')).toHaveLength(3)
  })

  test('appends an editable tier based on the last threshold and prevents deleting the final tier', async () => {
    const user = userEvent.setup()
    renderTiers()

    await user.click(screen.getByRole('button', { name: 'Add tier' }))

    const newRow = screen.getAllByRole('row').at(-1)
    expect(newRow).toBeDefined()
    const newFields = within(newRow as HTMLElement).getAllByRole('spinbutton')
    expect(newFields[0]).toHaveValue(400)
    expect(newFields[2]).toHaveValue(2)
    for (let remaining = 4; remaining > 1; remaining -= 1) {
      await user.click(screen.getAllByRole('button', { name: 'Delete' })[0])
    }
    expect(screen.getByRole('button', { name: 'Delete' })).toBeDisabled()
    expect(screen.getAllByRole('spinbutton')[0]).toHaveValue(400)
  })
})
