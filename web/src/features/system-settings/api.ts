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
import { api } from '@/lib/api'

import type {
  ConfirmPaymentComplianceResponse,
  ContentModerationConfig,
  ContentModerationLogPage,
  ContentModerationRuntime,
  ContentModerationTestResult,
  FetchUpstreamRatiosRequest,
  LogCleanupTask,
  PromptAuditProbeRequest,
  PromptAuditProbeResponse,
  PromptAuditConfig,
  PromptAuditEndpoint,
  PromptAuditEvent,
  PromptAuditEventFilter,
  PromptAuditEventPage,
  PromptAuditDeletePreview,
  PromptAuditDeleteResult,
  PromptAuditProbeResult,
  PromptAuditRuntime,
  SystemOptionsResponse,
  SystemTaskListResponse,
  SystemTaskResponse,
  UpdateOptionRequest,
  UpdateOptionResponse,
  UpstreamChannelsResponse,
  UpstreamRatiosResponse,
} from './types'

export async function getSystemOptions() {
  const res = await api.get<SystemOptionsResponse>('/api/option/')
  return res.data
}

export async function updateSystemOption(request: UpdateOptionRequest) {
  const res = await api.put<UpdateOptionResponse>('/api/option/', request)
  return res.data
}

export async function testPromptAudit(request: PromptAuditProbeRequest) {
  const res = await api.post<PromptAuditProbeResponse>(
    '/api/option/prompt_audit/test',
    request
  )
  return res.data
}

type PromptAuditResponse<T> = {
  success: boolean
  message?: string
  data: T
}

type ContentModerationResponse<T> = {
  success: boolean
  message?: string
  data: T
}

export async function getContentModerationConfig() {
  const res = await api.get<ContentModerationResponse<ContentModerationConfig>>(
    '/api/content-moderation/config'
  )
  return res.data.data
}

export async function updateContentModerationConfig(
  request: Omit<
    ContentModerationConfig,
    | 'api_key_count'
    | 'api_key_statuses'
    | 'config_version'
    | 'updated_at'
    | 'updated_by'
    | 'encryption_key_configured'
    | 'prompt_audit_active'
  > & {
    expected_config_version: number
    api_keys: string[]
    delete_api_key_hashes: string[]
    clear_api_keys: boolean
  }
) {
  const res = await api.put<ContentModerationResponse<ContentModerationConfig>>(
    '/api/content-moderation/config',
    request
  )
  return res.data.data
}

export async function testContentModerationKeys(request: {
  api_keys: string[]
  base_url: string
  model: string
  proxy_url: string
  timeout_ms: number
  prompt: string
  images: string[]
}) {
  const res = await api.post<
    ContentModerationResponse<ContentModerationTestResult>
  >('/api/content-moderation/test', request)
  return res.data.data
}

export async function getContentModerationRuntime() {
  const res = await api.get<
    ContentModerationResponse<ContentModerationRuntime>
  >('/api/content-moderation/runtime')
  return res.data.data
}

export async function listContentModerationLogs(
  params: Record<string, unknown>
) {
  const res = await api.get<
    ContentModerationResponse<ContentModerationLogPage>
  >('/api/content-moderation/logs', { params })
  return res.data.data
}

export async function unbanContentModerationUser(userID: number) {
  const res = await api.post<ContentModerationResponse<Record<string, number>>>(
    `/api/content-moderation/users/${userID}/unban`
  )
  return res.data.data
}

export async function deleteContentModerationHash(inputHash: string) {
  const res = await api.delete<
    ContentModerationResponse<{ input_hash: string; deleted: boolean }>
  >('/api/content-moderation/hashes', { data: { input_hash: inputHash } })
  return res.data.data
}

export async function clearContentModerationHashes() {
  const res = await api.delete<ContentModerationResponse<{ deleted: number }>>(
    '/api/content-moderation/hashes/all'
  )
  return res.data.data
}

export async function getPromptAuditConfig() {
  const res = await api.get<PromptAuditResponse<PromptAuditConfig>>(
    '/api/prompt-audit/config'
  )
  return res.data.data
}

export async function updatePromptAuditConfig(
  request: Omit<
    PromptAuditConfig,
    | 'config_version'
    | 'updated_at'
    | 'updated_by'
    | 'encryption_key_configured'
    | 'content_moderation_active'
  > & { expected_config_version: number }
) {
  const res = await api.put<PromptAuditResponse<PromptAuditConfig>>(
    '/api/prompt-audit/config',
    request
  )
  return res.data.data
}

export async function getPromptAuditRuntime() {
  const res = await api.get<PromptAuditResponse<PromptAuditRuntime>>(
    '/api/prompt-audit/runtime'
  )
  return res.data.data
}

export async function probePromptAuditEndpoint(endpoint: PromptAuditEndpoint) {
  const res = await api.post<PromptAuditResponse<PromptAuditProbeResult>>(
    '/api/prompt-audit/probe',
    endpoint
  )
  return res.data.data
}

export async function listPromptAuditEvents(params: Record<string, unknown>) {
  const res = await api.get<PromptAuditResponse<PromptAuditEventPage>>(
    '/api/prompt-audit/events',
    { params }
  )
  return res.data.data
}

export async function getPromptAuditEvent(id: number) {
  const res = await api.get<PromptAuditResponse<PromptAuditEvent>>(
    `/api/prompt-audit/events/${id}`
  )
  return res.data.data
}

export async function deletePromptAuditEvent(id: number) {
  const res = await api.delete<PromptAuditResponse<PromptAuditDeleteResult>>(
    `/api/prompt-audit/events/${id}`
  )
  return res.data.data
}

export async function batchDeletePromptAuditEvents(ids: number[]) {
  const res = await api.post<PromptAuditResponse<PromptAuditDeleteResult>>(
    '/api/prompt-audit/events/batch-delete',
    { ids }
  )
  return res.data.data
}

export async function previewPromptAuditEventDelete(
  filter: PromptAuditEventFilter
) {
  const res = await api.get<PromptAuditResponse<PromptAuditDeletePreview>>(
    '/api/prompt-audit/events/delete-preview',
    { params: filter }
  )
  return res.data.data
}

export async function deletePromptAuditEventsByFilter(
  filter: PromptAuditEventFilter,
  preview: PromptAuditDeletePreview
) {
  const res = await api.post<PromptAuditResponse<PromptAuditDeleteResult>>(
    '/api/prompt-audit/events/filter-delete',
    {
      filter,
      snapshot_max_id: preview.snapshot_max_id,
      filter_hash: preview.filter_hash,
      confirmation_token: preview.confirmation_token,
      confirm: true,
    }
  )
  return res.data.data
}

export async function confirmPaymentCompliance() {
  const res = await api.post<ConfirmPaymentComplianceResponse>(
    '/api/option/payment_compliance',
    { confirmed: true }
  )
  return res.data
}

export async function startLogCleanupTask(targetTimestamp: number) {
  const res = await api.post<SystemTaskResponse<LogCleanupTask>>(
    '/api/system-task/log-cleanup',
    null,
    {
      params: { target_timestamp: targetTimestamp },
    }
  )
  return res.data
}

export async function getCurrentLogCleanupTask() {
  const res = await api.get<SystemTaskResponse<LogCleanupTask | null>>(
    '/api/system-task/current',
    {
      params: { type: 'log_cleanup' },
    }
  )
  return res.data
}

export async function getSystemTask(taskId: string) {
  const res = await api.get<SystemTaskResponse<LogCleanupTask>>(
    `/api/system-task/${taskId}`
  )
  return res.data
}

export async function listSystemTasks(limit = 20) {
  const res = await api.get<SystemTaskListResponse>('/api/system-task/list', {
    params: { limit },
  })
  return res.data
}

export async function resetModelRatios() {
  const res = await api.post<UpdateOptionResponse>(
    '/api/option/rest_model_ratio'
  )
  return res.data
}

export async function getUpstreamChannels() {
  const res = await api.get<UpstreamChannelsResponse>(
    '/api/ratio_sync/channels'
  )
  return res.data
}

export async function fetchUpstreamRatios(request: FetchUpstreamRatiosRequest) {
  const res = await api.post<UpstreamRatiosResponse>(
    '/api/ratio_sync/fetch',
    request
  )
  return res.data
}
