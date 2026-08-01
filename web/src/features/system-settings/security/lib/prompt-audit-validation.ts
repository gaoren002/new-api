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

export const promptAuditWorkerCountRange = { min: 1, max: 64 } as const
export const promptAuditEndpointInputLimitRange = {
  min: 128,
  max: 2_000_000,
} as const

export type PromptAuditResourceLimitError =
  | { field: 'worker_count' }
  | { field: 'endpoint_input_limit'; endpointIndex: number }

export function validatePromptAuditResourceLimits(config: {
  worker_count: number
  endpoints: Array<{ input_limit: number }>
}): PromptAuditResourceLimitError | null {
  if (
    !Number.isInteger(config.worker_count) ||
    config.worker_count < promptAuditWorkerCountRange.min ||
    config.worker_count > promptAuditWorkerCountRange.max
  ) {
    return { field: 'worker_count' }
  }

  const endpointIndex = config.endpoints.findIndex(
    (endpoint) =>
      !Number.isInteger(endpoint.input_limit) ||
      endpoint.input_limit < promptAuditEndpointInputLimitRange.min ||
      endpoint.input_limit > promptAuditEndpointInputLimitRange.max
  )
  if (endpointIndex >= 0) {
    return { field: 'endpoint_input_limit', endpointIndex }
  }
  return null
}
