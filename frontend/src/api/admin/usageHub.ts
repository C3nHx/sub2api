/**
 * 管理端「用量 Hub」（CLIProxyAPI 兼容，全站 Codex OAuth 额度总览）API 模块
 */

import { apiClient } from '../client'

export interface UsageHubConfig {
  enabled: boolean
  include_disabled: boolean
  self_service_enabled: boolean
  allow_magpie: boolean
  expose_reset_credits: boolean
  active_self_service_users: number
  has_key: boolean
  masked_key: string
  generated_key?: string
  hub_path: string
  auth_files_path: string
  api_call_path: string
  supported_platforms: string[]
  allowed_upstream_urls: string[]
  cache_ttl_seconds: number
}

export interface UpdateUsageHubConfigPayload {
  enabled?: boolean
  include_disabled?: boolean
  self_service_enabled?: boolean
  allow_magpie?: boolean
  expose_reset_credits?: boolean
}

export interface UsageHubWindowProgress {
  utilization: number
  utilization_unknown?: boolean
  resets_at?: string | null
  remaining_seconds: number
}

export interface UsageHubResetCredits {
  available_count: number
  credits?: Array<{ expires_at?: string }>
}

export interface UsageHubCredits {
  has_credits: boolean
  unlimited: boolean
  balance: string | null
}

export interface UsageHubAccountItem {
  id: number
  name: string
  email?: string
  auth_index: string
  auth_file_name: string
  chatgpt_account_id?: string
  plan_type?: string
  status: string
  schedulable: boolean
  exposed_to_hub: boolean
  rate_limited: boolean
  rate_limit_reset_at?: string | null
  error_message?: string
  five_hour?: UsageHubWindowProgress | null
  seven_day?: UsageHubWindowProgress | null
  reset_credits?: UsageHubResetCredits | null
  credits?: UsageHubCredits | null
  updated_at?: string | null
  last_used_at?: string | null
}

export interface UsageHubSummary {
  total_accounts: number
  exposed_accounts: number
  healthy_accounts: number
  rate_limited_accounts: number
  error_accounts: number
  disabled_accounts: number
  known_5h_count: number
  known_7d_count: number
  avg_5h_used_percent?: number | null
  avg_7d_used_percent?: number | null
  total_reset_credits: number
}

export interface UsageHubOverviewResponse {
  summary: UsageHubSummary
  accounts: UsageHubAccountItem[]
}

export interface UsageHubBatchRefreshError {
  account_id: number
  name: string
  error: string
}

export interface UsageHubBatchRefreshResponse {
  refreshed: number
  failed: number
  errors?: UsageHubBatchRefreshError[]
  overview: UsageHubOverviewResponse
}

export interface UsageHubAuthFilesPreview {
  files: Array<Record<string, unknown>>
}

export async function getConfig(): Promise<UsageHubConfig> {
  const { data } = await apiClient.get<UsageHubConfig>('/admin/usage-hub/config')
  return data
}

export async function updateConfig(payload: UpdateUsageHubConfigPayload): Promise<UsageHubConfig> {
  const { data } = await apiClient.put<UsageHubConfig>('/admin/usage-hub/config', payload)
  return data
}

export async function regenerateKey(): Promise<{ key: string; masked_key: string }> {
  const { data } = await apiClient.post<{ key: string; masked_key: string }>('/admin/usage-hub/key/regenerate')
  return data
}

export async function deleteKey(): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>('/admin/usage-hub/key')
  return data
}

export async function listAccounts(): Promise<UsageHubOverviewResponse> {
  const { data } = await apiClient.get<UsageHubOverviewResponse>('/admin/usage-hub/accounts')
  return data
}

export async function refreshAccount(id: number): Promise<UsageHubAccountItem> {
  const { data } = await apiClient.post<UsageHubAccountItem>(`/admin/usage-hub/accounts/${id}/refresh`)
  return data
}

export async function refreshAllAccounts(accountIds?: number[]): Promise<UsageHubBatchRefreshResponse> {
  const body = accountIds && accountIds.length > 0 ? { account_ids: accountIds } : {}
  const { data } = await apiClient.post<UsageHubBatchRefreshResponse>('/admin/usage-hub/accounts/refresh-all', body)
  return data
}

export async function previewAuthFiles(): Promise<UsageHubAuthFilesPreview> {
  const { data } = await apiClient.get<UsageHubAuthFilesPreview>('/admin/usage-hub/preview-auth-files')
  return data
}

export const usageHubAPI = {
  getConfig,
  updateConfig,
  regenerateKey,
  deleteKey,
  listAccounts,
  refreshAccount,
  refreshAllAccounts,
  previewAuthFiles
}

export default usageHubAPI
