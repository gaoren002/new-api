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
export type SystemOption = {
  key: string
  value: string
}

export type SystemOptionKey = string

export type SystemOptionsResponse = {
  success: boolean
  message: string
  data: SystemOption[]
}

export type UpdateOptionRequest = {
  key: string
  value: string | boolean | number
}

export type UpdateOptionResponse = {
  success: boolean
  message: string
}

export type PromptAuditProbeRequest = {
  base_url: string
  model: string
  api_key: string
  timeout_ms: number
  input_limit: number
  max_concurrency: number
  scanners: string
}

export type PromptAuditProbeResponse = {
  success: boolean
  message: string
  latency_ms?: number
}

export type ConfirmPaymentComplianceResponse = {
  success: boolean
  message: string
  data?: {
    confirmed: boolean
    terms_version: string
    confirmed_at: number
    confirmed_by: number
  }
}

export type SystemTaskStatus = 'pending' | 'running' | 'succeeded' | 'failed'

export type SystemTask<
  TPayload = Record<string, unknown>,
  TState = Record<string, unknown>,
  TResult = Record<string, unknown>,
> = {
  id: number
  task_id: string
  type: string
  status: SystemTaskStatus
  active_key?: string
  payload?: TPayload
  state?: TState
  result?: TResult
  error?: string
  locked_by?: string
  locked_until?: number
  created_at: number
  updated_at: number
}

export type LogCleanupTaskPayload = {
  target_timestamp: number
  batch_size: number
}

export type LogCleanupTaskState = {
  total: number
  processed: number
  progress: number
  remaining: number
}

export type LogCleanupTaskResult = {
  deleted_count: number
}

export type LogCleanupTask = SystemTask<
  LogCleanupTaskPayload,
  LogCleanupTaskState,
  LogCleanupTaskResult
>

export type SystemTaskResponse<TTask = SystemTask | null> = {
  success: boolean
  message: string
  data?: TTask
}

export type SystemTaskListResponse = {
  success: boolean
  message: string
  data?: SystemTask[]
}

export type SiteSettings = {
  Notice: string
  SystemName: string
  Logo: string
  Footer: string
  About: string
  HomePageContent: string
  ServerAddress: string
  TaskPublicAddress: string
  'legal.user_agreement': string
  'legal.privacy_policy': string
  HeaderNavModules: string
  SidebarModulesAdmin: string
}

export type AuthSettings = {
  PasswordLoginEnabled: boolean
  PasswordRegisterEnabled: boolean
  EmailVerificationEnabled: boolean
  RegisterEnabled: boolean
  InviteCodeRegisterEnabled: boolean
  InviteCodeExpireMinutes: number
  InviteBotSecret: string
  EmailDomainRestrictionEnabled: boolean
  EmailAliasRestrictionEnabled: boolean
  EmailDomainWhitelist: string
  ServerAddress: string
  GitHubOAuthEnabled: boolean
  GitHubClientId: string
  GitHubClientSecret: string
  'discord.enabled': boolean
  'discord.client_id': string
  'discord.client_secret': string
  'oidc.enabled': boolean
  'oidc.display_name': string
  'oidc.client_id': string
  'oidc.client_secret': string
  'oidc.well_known': string
  'oidc.authorization_endpoint': string
  'oidc.token_endpoint': string
  'oidc.user_info_endpoint': string
  TelegramOAuthEnabled: boolean
  TelegramBotToken: string
  TelegramBotName: string
  LinuxDOOAuthEnabled: boolean
  LinuxDOClientId: string
  LinuxDOClientSecret: string
  LinuxDOMinimumTrustLevel: string
  WeChatAuthEnabled: boolean
  WeChatServerAddress: string
  WeChatServerToken: string
  WeChatAccountQRCodeImageURL: string
  TurnstileCheckEnabled: boolean
  TurnstileSiteKey: string
  TurnstileSecretKey: string
  'passkey.enabled': boolean
  'passkey.rp_display_name': string
  'passkey.rp_id': string
  'passkey.origins': string
  'passkey.allow_insecure_origin': boolean
  'passkey.user_verification': 'required' | 'preferred' | 'discouraged'
  'passkey.attachment_preference': '' | 'platform' | 'cross-platform'
}

export type ContentSettings = {
  'console_setting.api_info': string
  'console_setting.announcements': string
  'console_setting.faq': string
  'console_setting.uptime_kuma_groups': string
  'console_setting.api_info_enabled': boolean
  'console_setting.announcements_enabled': boolean
  'console_setting.faq_enabled': boolean
  'console_setting.uptime_kuma_enabled': boolean
  DataExportEnabled: boolean
  DataExportDefaultTime: string
  DataExportInterval: number
  Chats: string
  DrawingEnabled: boolean
  MjNotifyEnabled: boolean
  MjAccountFilterEnabled: boolean
  MjForwardUrlEnabled: boolean
  MjModeClearEnabled: boolean
  MjActionCheckSuccessEnabled: boolean
}

export type ModelSettings = {
  'global.pass_through_request_enabled': boolean
  'global.thinking_model_blacklist': string
  'global.chat_completions_to_responses_policy': string
  'general_setting.ping_interval_enabled': boolean
  'general_setting.ping_interval_seconds': number
  'gemini.safety_settings': string
  'gemini.version_settings': string
  'gemini.supported_imagine_models': string
  'gemini.thinking_adapter_enabled': boolean
  'gemini.thinking_adapter_budget_tokens_percentage': number
  'gemini.function_call_thought_signature_enabled': boolean
  'gemini.remove_function_response_id_enabled': boolean
  'claude.model_headers_settings': string
  'claude.default_max_tokens': string
  'claude.thinking_adapter_enabled': boolean
  'claude.thinking_adapter_budget_tokens_percentage': number
  'grok.violation_deduction_enabled': boolean
  'grok.violation_deduction_amount': number
  ModelPrice: string
  ModelRatio: string
  CacheRatio: string
  CreateCacheRatio: string
  CompletionRatio: string
  ImageRatio: string
  AudioRatio: string
  AudioCompletionRatio: string
  ExposeRatioEnabled: boolean
  'billing_setting.billing_mode': string
  'billing_setting.billing_expr': string
  'tool_price_setting.prices': string
  TopupGroupRatio: string
  GroupRatio: string
  UserUsableGroups: string
  GroupGroupRatio: string
  AutoGroups: string
  MaxTokenAutoGroups: number
  DefaultUseAutoGroup: boolean
  'group_ratio_setting.group_special_usable_group': string
  RetryTimes: number
  ChannelDisableThreshold: string
  AutomaticDisableChannelEnabled: boolean
  AutomaticEnableChannelEnabled: boolean
  AutomaticDisableKeywords: string
  AutomaticDisableStatusCodes: string
  AutomaticRetryStatusCodes: string
  'monitor_setting.auto_test_channel_enabled': boolean
  'monitor_setting.auto_test_channel_minutes': number
  'monitor_setting.channel_test_concurrency': number
  'monitor_setting.channel_test_mode':
    | 'scheduled_all'
    | 'auto_ban_only'
    | 'passive_recovery'
  'channel_affinity_setting.enabled': boolean
  'channel_affinity_setting.switch_on_success': boolean
  'channel_affinity_setting.keep_on_channel_disabled': boolean
  'channel_affinity_setting.max_entries': number
  'channel_affinity_setting.default_ttl_seconds': number
  'channel_affinity_setting.rules': string
  'model_deployment.ionet.api_key': string
  'model_deployment.ionet.enabled': boolean
}

export type BillingSettings = {
  QuotaForNewUser: number
  PreConsumedQuota: number
  'data_consent.enabled': boolean
  'data_consent.accepted_multiplier': number
  'data_consent.declined_multiplier': number
  'data_consent.agreement_version': string
  'data_consent.agreement_content': string
  QuotaForInviter: number
  QuotaForInvitee: number
  TopUpLink: string
  'general_setting.docs_link': string
  'quota_setting.enable_free_model_pre_consume': boolean
  QuotaPerUnit: number
  USDExchangeRate: number
  'general_setting.quota_display_type': string
  'general_setting.custom_currency_symbol': string
  'general_setting.custom_currency_exchange_rate': number
  DisplayInCurrencyEnabled: boolean
  DisplayTokenStatEnabled: boolean
  ModelPrice: string
  ModelRatio: string
  CacheRatio: string
  CreateCacheRatio: string
  CompletionRatio: string
  ImageRatio: string
  AudioRatio: string
  AudioCompletionRatio: string
  ExposeRatioEnabled: boolean
  'billing_setting.billing_mode': string
  'billing_setting.billing_expr': string
  'tool_price_setting.prices': string
  TopupGroupRatio: string
  GroupRatio: string
  UserUsableGroups: string
  GroupGroupRatio: string
  AutoGroups: string
  MaxTokenAutoGroups: number
  DefaultUseAutoGroup: boolean
  'group_ratio_setting.group_special_usable_group': string
  PayAddress: string
  EpayId: string
  EpayKey: string
  Price: number
  MinTopUp: number
  CustomCallbackAddress: string
  PayMethods: string
  'payment_setting.amount_options': string
  'payment_setting.amount_discount': string
  'payment_setting.compliance_confirmed': boolean
  'payment_setting.compliance_terms_version': string
  'payment_setting.compliance_confirmed_at': number
  'payment_setting.compliance_confirmed_by': number
  'payment_setting.compliance_confirmed_ip': string
  StripeApiSecret: string
  StripeWebhookSecret: string
  StripePriceId: string
  StripeUnitPrice: number
  StripeMinTopUp: number
  StripePromotionCodesEnabled: boolean
  CreemApiKey: string
  CreemWebhookSecret: string
  CreemTestMode: boolean
  CreemProducts: string
  WaffoEnabled: boolean
  WaffoApiKey: string
  WaffoPrivateKey: string
  WaffoPublicCert: string
  WaffoSandboxPublicCert: string
  WaffoSandboxApiKey: string
  WaffoSandboxPrivateKey: string
  WaffoSandbox: boolean
  WaffoMerchantId: string
  WaffoCurrency: string
  WaffoUnitPrice: number
  WaffoMinTopUp: number
  WaffoNotifyUrl: string
  WaffoReturnUrl: string
  WaffoPayMethods: string
  WaffoPancakeMerchantID: string
  WaffoPancakePrivateKey: string
  WaffoPancakeReturnURL: string
  // Bound by the operator through the catalog flow in the admin Pancake
  // section (saved via /api/option/waffo-pancake/save).
  WaffoPancakeStoreID: string
  WaffoPancakeProductID: string
}

export type OperationsSettings = {
  DefaultCollapseSidebar: boolean
  DemoSiteEnabled: boolean
  SelfUseModeEnabled: boolean
  QuotaRemindThreshold: string
  'checkin_setting.enabled': boolean
  'checkin_setting.min_quota': number
  'checkin_setting.max_quota': number
  'checkin_setting.tiered': boolean
  'checkin_setting.tiers': string
  'checkin_setting.fallback_mode': 'legacy' | 'none'
  QuotaPerUnit: number
  USDExchangeRate: number
  'general_setting.quota_display_type': string
  'general_setting.custom_currency_symbol': string
  'general_setting.custom_currency_exchange_rate': number
  SMTPServer: string
  SMTPPort: string
  SMTPAccount: string
  SMTPFrom: string
  SMTPToken: string
  SMTPSSLEnabled: boolean
  SMTPStartTLSEnabled: boolean
  SMTPInsecureSkipVerify: boolean
  SMTPForceAuthLogin: boolean
  WorkerUrl: string
  WorkerValidKey: string
  WorkerAllowHttpImageRequestEnabled: boolean
  LogConsumeEnabled: boolean
  'performance_setting.disk_cache_enabled': boolean
  'performance_setting.disk_cache_threshold_mb': number
  'performance_setting.disk_cache_max_size_mb': number
  'performance_setting.disk_cache_path': string
  'performance_setting.monitor_enabled': boolean
  'performance_setting.monitor_cpu_threshold': number
  'performance_setting.monitor_memory_threshold': number
  'performance_setting.monitor_disk_threshold': number
  'perf_metrics_setting.enabled': boolean
  'perf_metrics_setting.flush_interval': number
  'perf_metrics_setting.bucket_time': 'hour' | 'minute' | '5min'
  'perf_metrics_setting.retention_days': number
}

export type SecuritySettings = {
  ModelRequestRateLimitEnabled: boolean
  ModelRequestRateLimitCount: number
  ModelRequestRateLimitSuccessCount: number
  ModelRequestRateLimitDurationMinutes: number
  ModelRequestRateLimitGroup: string
  CheckSensitiveEnabled: boolean
  CheckSensitiveOnPromptEnabled: boolean
  SensitiveWords: string
  PromptAuditEnabled: boolean
  PromptAuditBaseURL: string
  PromptAuditModel: string
  PromptAuditTimeoutMS: number
  PromptAuditFailClosed: boolean
  PromptAuditInputLimit: number
  PromptAuditMaxConcurrency: number
  PromptAuditScanners: string
  PromptAuditGroupPolicies: string
  PromptAuditKeyConfigured: boolean
  GroupRatio: string
  'fetch_setting.enable_ssrf_protection': boolean
  'fetch_setting.allow_private_ip': boolean
  'fetch_setting.domain_filter_mode': boolean
  'fetch_setting.ip_filter_mode': boolean
  'fetch_setting.domain_list': string[]
  'fetch_setting.ip_list': string[]
  'fetch_setting.allowed_ports': number[]
  'fetch_setting.apply_ip_filter_for_domain': boolean
  'token_setting.max_user_tokens': number
}

export type PromptAuditMode = 'off' | 'async_audit' | 'blocking'

export type PromptAuditEndpoint = {
  id: string
  name: string
  protocol: 'openai_compatible'
  base_url: string
  model: string
  timeout_ms: number
  input_limit: number
  enabled: boolean
  has_token: boolean
  token_status: string
  token?: string
  clear_token?: boolean
}

export type PromptAuditGroupPolicyConfig = {
  mode: PromptAuditMode
  enabled: boolean
  fail_closed: boolean
  scanners: string[]
}

export type PromptAuditConfig = {
  mode: PromptAuditMode
  blocking_latest_turn_only: boolean
  store_pass_events: boolean
  store_blocked_events_only: boolean
  strategy: 'priority'
  worker_count: number
  queue_capacity: number
  scanners: string[]
  fail_closed: boolean
  endpoints: PromptAuditEndpoint[]
  group_policies: Record<string, PromptAuditGroupPolicyConfig>
  config_version: number
  updated_at: string
  updated_by: number
  encryption_key_configured: boolean
  content_moderation_active: boolean
}

export type ContentModerationMode = 'off' | 'observe' | 'pre_block'

export type ContentModerationAPIKeyStatus = {
  index: number
  key_hash: string
  masked: string
  status: string
  failure_count: number
  success_count: number
  last_error: string
  last_checked_at?: string
  frozen_until?: string
  last_latency_ms: number
  last_http_status: number
  active: number
  total_calls: number
  error_count: number
  average_latency_ms: number
  configured: boolean
}

export type ContentModerationModelFilter = {
  type: 'all' | 'include' | 'exclude'
  models: string[]
}

export type ContentModerationConfig = {
  enabled: boolean
  mode: ContentModerationMode
  base_url: string
  model: string
  proxy_url: string
  api_key_count: number
  api_key_statuses: ContentModerationAPIKeyStatus[]
  timeout_ms: number
  sample_rate: number
  all_groups: boolean
  groups: string[]
  record_non_hits: boolean
  thresholds: Record<string, number>
  worker_count: number
  queue_size: number
  block_status: number
  block_message: string
  email_on_hit: boolean
  auto_ban_enabled: boolean
  ban_threshold: number
  violation_window_hours: number
  retry_count: number
  hit_retention_days: number
  non_hit_retention_days: number
  pre_hash_check_enabled: boolean
  blocked_keywords: string[]
  keyword_blocking_mode: 'keyword_only' | 'keyword_and_api' | 'api_only'
  model_filter: ContentModerationModelFilter
  cyber_policy_exclude_from_ban_count: boolean
  cyber_session_block_enabled: boolean
  cyber_session_block_ttl_seconds: number
  config_version: number
  updated_at: string
  updated_by: number
  encryption_key_configured: boolean
  prompt_audit_active: boolean
}

export type ContentModerationRuntime = {
  enabled: boolean
  mode: ContentModerationMode
  worker_count: number
  max_workers: number
  active_workers: number
  queue_size: number
  queue_length: number
  queue_usage_percent: number
  enqueued: number
  dropped: number
  processed: number
  errors: number
  pre_block_active: number
  pre_block_checked: number
  pre_block_allowed: number
  pre_block_blocked: number
  pre_block_errors: number
  pre_block_avg_latency_ms: number
  pre_block_api_key_active: number
  pre_block_api_key_available_count: number
  pre_block_api_key_total_calls: number
  api_key_statuses: ContentModerationAPIKeyStatus[]
  flagged_hash_count: number
  last_cleanup_at?: string
  last_cleanup_deleted_hit: number
  last_cleanup_deleted_non_hit: number
  config_version: number
  prompt_audit_active: boolean
  encryption_key_configured: boolean
}

export type ContentModerationLog = {
  id: number
  created_at: number
  request_id: string
  user_id: number
  username: string
  user_email: string
  token_id: number
  token_name: string
  group: string
  endpoint: string
  provider: string
  protocol: string
  model: string
  mode: ContentModerationMode
  action: string
  flagged: boolean
  highest_category: string
  highest_score: number
  matched_keyword: string
  category_scores: Record<string, number>
  threshold_snapshot: Record<string, number>
  input_hash: string
  input_excerpt: string
  upstream_latency_ms?: number
  queue_delay_ms?: number
  error: string
  violation_count: number
  auto_banned: boolean
  email_sent: boolean
  user_status: number
}

export type ContentModerationLogPage = {
  items: ContentModerationLog[]
  total: number
  page: number
  page_size: number
}

export type ContentModerationTestResult = {
  items: ContentModerationAPIKeyStatus[]
  audit_result?: {
    allowed: boolean
    blocked: boolean
    flagged: boolean
    highest_category: string
    highest_score: number
    composite_score: number
    category_scores: Record<string, number>
    thresholds: Record<string, number>
  }
  image_count: number
}

export type PromptAuditRuntime = {
  process_status: string
  effective_mode: PromptAuditMode
  expected_config_version: number
  active_config_version: number
  config_loaded_at?: string
  config_load_error?: string
  worker_total: number
  worker_active: number
  worker_heartbeat_at?: string
  last_processed_at?: string
  queue_capacity: number
  queue: Record<string, number>
  database_status: string
  redis_status: string
  last_error_code?: string
  last_error_message?: string
  endpoints: Record<string, PromptAuditProbeResult>
  guard_metrics: Record<string, number>
}

export type PromptAuditProbeResult = {
  ok: boolean
  status: string
  error_code?: string
  message: string
  latency_ms: number
  http_status: number
  retryable: boolean
  checked_at: string
  token_applied: boolean
}

export type PromptAuditEvent = {
  id: number
  created_at: number
  request_id: string
  user_id: number
  username: string
  user_email: string
  token_id: number
  token_name: string
  group: string
  provider: string
  endpoint: string
  protocol: string
  model: string
  redacted_preview: string
  prompt_hash: string
  full_prompt: string
  prompt_length: number
  message_count: number
  stage: string
  decision: 'pass' | 'flag' | 'critical'
  risk_level: 'low' | 'medium' | 'high' | 'critical'
  action: string
  safety: string
  categories: string[]
  matched_scanners: string[]
  unknown_categories: string[]
  scanner_scores: Record<string, number>
  scanner_evidence: Record<string, string>
  issue_summaries: PromptAuditIssueSummary[]
  scanner_backend: string
  scanner_version: string
  guard_endpoint_id: string
  config_version: number
  policy_id: string
  policy_version: number
  latency_ms: number
  chunk_total: number
}

export type PromptAuditIssueSummary = {
  category: string
  scanner_id: string
  title: string
  description?: string
  severity: string
  severity_label?: string
  action: string
  action_label?: string
  code: string
  score: number
  evidence?: string
  evidence_hash?: string
}

export type PromptAuditEventFilter = {
  decision?: string
  risk_level?: string
  group?: string
  user_id?: number
  token_id?: number
  model?: string
  request_id?: string
  prompt_hash?: string
  endpoint?: string
  keyword?: string
  start_time?: number
  end_time?: number
}

export type PromptAuditDeleteResult = {
  deleted_events: number
  deleted_jobs: number
}

export type PromptAuditDeletePreview = {
  matched_count: number
  filter_summary: PromptAuditEventFilter
  snapshot_max_id: number
  filter_hash: string
  confirmation_token: string
  expires_at: string
}

export type PromptAuditEventPage = {
  items: PromptAuditEvent[]
  total: number
  page: number
  page_size: number
  pages: number
}

export type UpstreamChannel = {
  id: number
  name: string
  base_url: string
  status: number
  type?: number
}

export type RatioType =
  | 'model_ratio'
  | 'completion_ratio'
  | 'cache_ratio'
  | 'create_cache_ratio'
  | 'image_ratio'
  | 'audio_ratio'
  | 'audio_completion_ratio'
  | 'model_price'
  | 'billing_mode'
  | 'billing_expr'

export type RatioDifference = {
  current: number | string | null
  upstreams: Record<string, number | string | 'same'>
  confidence: Record<string, boolean>
}

export type DifferencesMap = Record<
  string,
  Partial<Record<RatioType, RatioDifference>>
>

export type UpstreamChannelsResponse = {
  success: boolean
  message: string
  data: UpstreamChannel[]
}

export type UpstreamConfig = {
  id: number
  name: string
  base_url: string
  endpoint: string
}

export type FetchUpstreamRatiosRequest = {
  upstreams: UpstreamConfig[]
  timeout: number
}

export type TestResult = {
  name: string
  status: 'success' | 'error'
  error?: string
}

export type UpstreamRatiosResponse = {
  success: boolean
  message: string
  data: {
    differences: DifferencesMap
    test_results: TestResult[]
  }
}
