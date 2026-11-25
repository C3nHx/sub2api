/**
 * 用户自助「用量 Hub」API 模块（个人独立只读 Token 与自定义 Magpie 数据源 CPA 整形）
 */

import { apiClient } from './client'

export type UsageHubMagpiePlatform = 'codex' | 'claude' | 'antigravity'

export interface UsageHubMagpieSourceView {
  id: string
  name: string
  enabled: boolean
  base_url: string
  has_api_key: boolean
  masked_api_key: string
  providers: UsageHubMagpiePlatform[]
}

export interface UsageHubMagpieSourceInput {
  id?: string
  name: string
  enabled: boolean
  base_url: string
  api_key?: string
  providers: UsageHubMagpiePlatform[]
}

export interface UserUsageHubConfig {
  self_service_enabled: boolean
  allow_magpie: boolean
  allow_private_magpie_url: boolean
  is_admin: boolean
  enabled: boolean
  has_key: boolean
  masked_key: string
  generated_key?: string
  include_sub2api: boolean
  include_disabled: boolean
  expose_reset_credits: boolean
  hub_path: string
  auth_files_path: string
  api_call_path: string
  supported_platforms: string[]
  magpie_platforms: UsageHubMagpiePlatform[]
  max_magpie_sources: number
  cache_ttl_seconds: number
  magpie_sources: UsageHubMagpieSourceView[]
}

export interface UpdateUserUsageHubConfigPayload {
  enabled?: boolean
  include_sub2api?: boolean
  include_disabled?: boolean
  expose_reset_credits?: boolean
  magpie_sources?: UsageHubMagpieSourceInput[]
}

export interface TestMagpieSourcePayload {
  source_id?: string
  base_url: string
  api_key?: string
  providers?: UsageHubMagpiePlatform[]
}

export interface TestMagpieSourceResult {
  ok: boolean
  normalized_url?: string
  total_raw_quotas: number
  matched_count: number
  provider_counts: Record<string, number>
  sample_labels: string[]
  error?: string
}

export interface UsageHubPreviewFile {
  id: string
  auth_index: string
  name: string
  type: string
  provider: string
  label: string
  status: string
  status_message?: string
  disabled: boolean
  unavailable: boolean
  source: string
  email?: string
  account?: string
  plan_type?: string
  project_id?: string
  updated_at?: string
}

export interface UsageHubPreviewWarning {
  source_id: string
  source_name: string
  message: string
}

export interface UserUsageHubPreviewResponse {
  files: UsageHubPreviewFile[]
  warnings?: UsageHubPreviewWarning[]
}

export async function getConfig(): Promise<UserUsageHubConfig> {
  const { data } = await apiClient.get<UserUsageHubConfig>('/user/usage-hub/config')
  return data
}

export async function updateConfig(
  payload: UpdateUserUsageHubConfigPayload
): Promise<UserUsageHubConfig> {
  const { data } = await apiClient.put<UserUsageHubConfig>('/user/usage-hub/config', payload)
  return data
}

export async function regenerateKey(): Promise<{ key: string; masked_key: string }> {
  const { data } = await apiClient.post<{ key: string; masked_key: string }>(
    '/user/usage-hub/key/regenerate'
  )
  return data
}

export async function deleteKey(): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>('/user/usage-hub/key')
  return data
}

export async function testMagpieSource(
  payload: TestMagpieSourcePayload
): Promise<TestMagpieSourceResult> {
  const { data } = await apiClient.post<TestMagpieSourceResult>(
    '/user/usage-hub/magpie/test',
    payload
  )
  return data
}

export async function previewAuthFiles(): Promise<UserUsageHubPreviewResponse> {
  const { data } = await apiClient.get<UserUsageHubPreviewResponse>(
    '/user/usage-hub/preview-auth-files'
  )
  return data
}

export const usageHubAPI = {
  getConfig,
  updateConfig,
  regenerateKey,
  deleteKey,
  testMagpieSource,
  previewAuthFiles
}

export default usageHubAPI
