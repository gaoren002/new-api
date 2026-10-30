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
import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'

import { requireServerSuccess } from '@/lib/server-error-message'

import { getChannelCircuits } from '../api'
import type { ChannelCircuit } from '../types'

const CIRCUITS_REFRESH_MS = 30_000
const DEFAULT_PROBE_SECONDS = 60

export type ChannelCircuits = {
  byChannelId: Map<number, ChannelCircuit>
  probeSeconds: number
}

/**
 * Open channel circuits of the instance serving the console. The list refreshes
 * in the background so the channel table shows which channels are skipped;
 * a failed refresh only leaves the badges out.
 */
export function useChannelCircuits(): ChannelCircuits {
  const query = useQuery({
    queryKey: ['channel-circuits'],
    queryFn: async () => requireServerSuccess(await getChannelCircuits()),
    refetchInterval: CIRCUITS_REFRESH_MS,
    retry: false,
    meta: { errorToast: false },
  })
  const data = query.data?.data
  return useMemo(
    () => ({
      byChannelId: new Map(
        (data?.items ?? []).map((circuit) => [circuit.channel_id, circuit])
      ),
      probeSeconds: data?.probe_seconds ?? DEFAULT_PROBE_SECONDS,
    }),
    [data]
  )
}
