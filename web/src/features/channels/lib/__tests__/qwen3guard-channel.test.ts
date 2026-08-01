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
import { describe, expect, test } from 'vitest'

import {
  CHANNEL_TYPES,
  CHANNEL_TYPE_OPTIONS,
  MODEL_FETCHABLE_TYPES,
} from '../../constants'
import { getChannelTypeConfig } from '../channel-type-config'
import { getChannelTypeIcon } from '../channel-utils'

const CHANNEL_TYPE_QWEN3GUARD = 62

describe('Qwen3Guard channel', () => {
  test('registers selection, ordering, model discovery, and icon metadata', () => {
    expect(CHANNEL_TYPES[CHANNEL_TYPE_QWEN3GUARD]).toBe('Qwen3Guard')
    expect(CHANNEL_TYPES[61]).toBe('Task Plugin')

    const option = CHANNEL_TYPE_OPTIONS.find(
      (item) => item.value === CHANNEL_TYPE_QWEN3GUARD
    )

    expect(option).toEqual({
      value: CHANNEL_TYPE_QWEN3GUARD,
      label: 'Qwen3Guard',
    })
    expect(MODEL_FETCHABLE_TYPES.has(CHANNEL_TYPE_QWEN3GUARD)).toBe(true)
    expect(MODEL_FETCHABLE_TYPES.has(61)).toBe(false)
    expect(getChannelTypeIcon(CHANNEL_TYPE_QWEN3GUARD)).toBe('Qwen')
  })

  test('exposes moderation-endpoint defaults in its type config', () => {
    const config = getChannelTypeConfig(CHANNEL_TYPE_QWEN3GUARD)

    expect(config.name).toBe('Qwen3Guard')
    expect(config.icon).toBe('Qwen')
    expect(config.defaultBaseUrl).toBe('http://localhost:11434')
  })
})
