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

import { validatePromptAuditResourceLimits } from '../prompt-audit-validation'

describe('prompt audit resource limit validation', () => {
  test('accepts the maximum worker count and endpoint chunk size', () => {
    expect(
      validatePromptAuditResourceLimits({
        worker_count: 64,
        endpoints: [{ input_limit: 2_000_000 }],
      })
    ).toBeNull()
  })

  test('identifies an out-of-range worker count before save', () => {
    expect(
      validatePromptAuditResourceLimits({
        worker_count: 65,
        endpoints: [{ input_limit: 8_000 }],
      })
    ).toEqual({ field: 'worker_count' })
  })

  test('identifies the out-of-range endpoint before save', () => {
    expect(
      validatePromptAuditResourceLimits({
        worker_count: 4,
        endpoints: [{ input_limit: 8_000 }, { input_limit: 2_000_001 }],
      })
    ).toEqual({ field: 'endpoint_input_limit', endpointIndex: 1 })
  })
})
