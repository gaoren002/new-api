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
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { channelSchema, type Channel, type ChannelCircuit } from '../../types'
import { ChannelCard } from '../channel-card'
import { useChannelsColumns } from '../channels-columns'
import { ChannelsProvider } from '../channels-provider'

function ExampleChannelCard(props: { channel: Channel }) {
  const table = useReactTable({
    data: [props.channel],
    columns: useChannelsColumns({ enableSelection: false }),
    getCoreRowModel: getCoreRowModel(),
  })
  const row = table.getRowModel().rows[0]
  return row ? <ChannelCard row={row} isSelected={false} /> : null
}

function renderEnabledChannelCard(circuits: ChannelCircuit[]) {
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url !== '/api/channel/circuits') {
      return { data: { success: true, data: [] } }
    }
    return {
      data: {
        success: true,
        data: { failure_threshold: 3, probe_seconds: 60, items: circuits },
      },
    }
  })
  const queryClient = new QueryClient()
  const channel = channelSchema.parse({
    id: 7,
    type: 1,
    key: 'test-key',
    name: 'Card-Channel',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })
  render(
    <QueryClientProvider client={queryClient}>
      <ChannelsProvider>
        <ExampleChannelCard channel={channel} />
      </ChannelsProvider>
    </QueryClientProvider>
  )
  return queryClient
}

afterEach(() => {
  vi.restoreAllMocks()
})

test('the card of an enabled channel with an open circuit shows the circuit badge and its models', async () => {
  renderEnabledChannelCard([
    {
      channel_id: 7,
      model: 'claude-opus-4-6',
      failures: 5,
      last_failure: 'status_code=503',
      opened_at: 1_793_000_000,
    },
  ])
  const user = userEvent.setup()

  await user.hover(await screen.findByText('Circuit open'))

  expect(await screen.findByText('claude-opus-4-6')).toBeVisible()
})

test('the card of an enabled channel without an open circuit shows no status badge', async () => {
  const queryClient = renderEnabledChannelCard([])

  await waitFor(() =>
    expect(api.get).toHaveBeenCalledWith(
      '/api/channel/circuits',
      expect.anything()
    )
  )
  await waitFor(() => expect(queryClient.isFetching()).toBe(0))
  expect(screen.queryByText('Circuit open')).not.toBeInTheDocument()
  expect(screen.queryByText('Enabled')).not.toBeInTheDocument()
})
