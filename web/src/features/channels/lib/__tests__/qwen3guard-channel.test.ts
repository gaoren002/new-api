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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  CHANNEL_TYPES,
  CHANNEL_TYPE_OPTIONS,
  MODEL_FETCHABLE_TYPES,
} from '../../constants'
import { getChannelTypeConfig } from '../channel-type-config'
import { getChannelTypeIcon } from '../channel-utils'

const CHANNEL_TYPE_QWEN3GUARD = 61

describe('Qwen3Guard channel', () => {
  test('registers selection, ordering, model discovery, and icon metadata', () => {
    assert.equal(CHANNEL_TYPES[CHANNEL_TYPE_QWEN3GUARD], 'Qwen3Guard')

    const option = CHANNEL_TYPE_OPTIONS.find(
      (item) => item.value === CHANNEL_TYPE_QWEN3GUARD
    )

    assert.deepEqual(option, {
      value: CHANNEL_TYPE_QWEN3GUARD,
      label: 'Qwen3Guard',
    })
    assert.equal(MODEL_FETCHABLE_TYPES.has(CHANNEL_TYPE_QWEN3GUARD), true)
    assert.equal(getChannelTypeIcon(CHANNEL_TYPE_QWEN3GUARD), 'Qwen')
  })

  test('exposes moderation-endpoint defaults in its type config', () => {
    const config = getChannelTypeConfig(CHANNEL_TYPE_QWEN3GUARD)

    assert.equal(config.name, 'Qwen3Guard')
    assert.equal(config.icon, 'Qwen')
    assert.equal(config.defaultBaseUrl, 'http://localhost:11434')
  })
})
