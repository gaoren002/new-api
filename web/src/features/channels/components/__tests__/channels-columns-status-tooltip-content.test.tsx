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
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { channelSchema, type Channel } from '../../types'
import { useChannelsColumns } from '../channels-columns'
import { ChannelsProvider } from '../channels-provider'

function ExampleAutoDisabledStatusCell(props: { channel: Channel }) {
  const table = useReactTable({
    data: [props.channel],
    columns: useChannelsColumns({ enableSelection: false }),
    getCoreRowModel: getCoreRowModel(),
  })
  const cell = table
    .getRowModel()
    .rows[0]?.getAllCells()
    .find((item) => item.column.id === 'status')

  return cell ? flexRender(cell.column.columnDef.cell, cell.getContext()) : null
}

test('keeps the long string inside the status tooltip is wrapped when show', async () => {
  const reason = '114514'.repeat(40)
  const channelItem = channelSchema.parse({
    id: 1,
    type: 1,
    key: 'test-key',
    name: 'Test-Channel-Item',
    status: 3,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    other_info: JSON.stringify({ status_reason: reason }),
  })
  const queryClient = new QueryClient()
  const user = userEvent.setup()

  render(
    <QueryClientProvider client={queryClient}>
      <ChannelsProvider>
        <ExampleAutoDisabledStatusCell channel={channelItem} />
      </ChannelsProvider>
    </QueryClientProvider>
  )

  await user.hover(screen.getByText('Auto Disabled'))

  expect(await screen.findByText(reason, { exact: false })).toHaveClass(
    'wrap-anywhere'
  )
})

afterEach(() => {
  vi.restoreAllMocks()
})

test('an enabled channel with open circuits lists each model it is skipped for', async () => {
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url !== '/api/channel/circuits') {
      return { data: { success: true, data: [] } }
    }
    return {
      data: {
        success: true,
        data: {
          failure_threshold: 3,
          probe_seconds: 60,
          items: [
            {
              channel_id: 7,
              model: 'claude-fable-5-1',
              failures: 3,
              last_failure: 'first_token_timeout (504)',
              opened_at: 1_793_000_000,
            },
            {
              channel_id: 7,
              model: 'claude-sonnet-4-6',
              failures: 4,
              last_failure: 'bad_response_status_code (503)',
              opened_at: 1_793_000_100,
            },
            {
              channel_id: 9,
              model: 'gpt-4o',
              failures: 3,
              last_failure: 'another channel',
              opened_at: 1_793_000_200,
            },
          ],
        },
      },
    }
  })
  const channelItem = channelSchema.parse({
    id: 7,
    type: 1,
    key: 'test-key',
    name: 'Broken-Channel',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })
  const queryClient = new QueryClient()
  const user = userEvent.setup()

  render(
    <QueryClientProvider client={queryClient}>
      <ChannelsProvider>
        <ExampleAutoDisabledStatusCell channel={channelItem} />
      </ChannelsProvider>
    </QueryClientProvider>
  )

  await user.hover(await screen.findByText('Circuit open'))

  expect(await screen.findByText('claude-fable-5-1')).toBeVisible()
  expect(screen.getByText('claude-sonnet-4-6')).toBeVisible()
  expect(
    screen.getByText('first_token_timeout (504)', { exact: false })
  ).toBeVisible()
  expect(screen.queryByText('gpt-4o')).not.toBeInTheDocument()
  expect(screen.queryByText('Enabled')).not.toBeInTheDocument()
})

test('an enabled channel without an open circuit keeps the enabled badge', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: { failure_threshold: 3, probe_seconds: 60, items: [] },
    },
  })
  const channelItem = channelSchema.parse({
    id: 8,
    type: 1,
    key: 'test-key',
    name: 'Healthy-Channel',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })
  const queryClient = new QueryClient()

  render(
    <QueryClientProvider client={queryClient}>
      <ChannelsProvider>
        <ExampleAutoDisabledStatusCell channel={channelItem} />
      </ChannelsProvider>
    </QueryClientProvider>
  )

  await waitFor(() =>
    expect(api.get).toHaveBeenCalledWith(
      '/api/channel/circuits',
      expect.anything()
    )
  )
  await waitFor(() => expect(queryClient.isFetching()).toBe(0))
  expect(screen.getByText('Enabled')).toBeVisible()
  expect(screen.queryByText('Circuit open')).not.toBeInTheDocument()
})
