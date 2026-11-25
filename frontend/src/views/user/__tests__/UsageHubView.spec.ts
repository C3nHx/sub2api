import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

import UserUsageHubView from '../UsageHubView.vue'

const {
  getConfig,
  updateConfig,
  regenerateKey,
  deleteKey,
  testMagpieSource,
  previewAuthFiles,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  getConfig: vi.fn(),
  updateConfig: vi.fn(),
  regenerateKey: vi.fn(),
  deleteKey: vi.fn(),
  testMagpieSource: vi.fn(),
  previewAuthFiles: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

vi.mock('@/api', () => ({
  usageHubAPI: {
    getConfig,
    updateConfig,
    regenerateKey,
    deleteKey,
    testMagpieSource,
    previewAuthFiles
  }
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showSuccess,
    showError
  })
}))

vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: defineComponent({
    name: 'AppLayoutStub',
    setup(_, { slots }) {
      return () => h('div', { class: 'app-layout-stub' }, slots.default?.())
    }
  })
}))

describe('User UsageHubView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getConfig.mockResolvedValue({
      self_service_enabled: true,
      allow_magpie: true,
      allow_private_magpie_url: false,
      is_admin: false,
      enabled: false,
      has_key: false,
      masked_key: '',
      include_sub2api: true,
      include_disabled: false,
      expose_reset_credits: false,
      hub_path: '/v0/management',
      auth_files_path: '/v0/management/auth-files',
      api_call_path: '/v0/management/api-call',
      supported_platforms: ['codex'],
      magpie_platforms: ['codex', 'claude', 'antigravity'],
      max_magpie_sources: 5,
      cache_ttl_seconds: 30,
      magpie_sources: []
    })
    previewAuthFiles.mockResolvedValue({
      files: [
        {
          id: 'codex-site@example.com-1.json',
          auth_index: 'sub2api-codex-1',
          name: 'codex-site@example.com-1.json',
          type: 'codex',
          provider: 'codex',
          label: 'Site Codex #1',
          status: 'active',
          disabled: false,
          unavailable: false,
          source: 'memory',
          email: 'site@example.com',
          plan_type: 'plus'
        }
      ],
      warnings: []
    })
  })

  it('loads personal config and renders merged preview files', async () => {
    const wrapper = mount(UserUsageHubView)
    await flushPromises()

    expect(getConfig).toHaveBeenCalledTimes(1)
    expect(previewAuthFiles).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="preview-file-sub2api-codex-1"]').exists()).toBe(true)
  })

  it('enables personal hub and reveals generated hubu token once', async () => {
    updateConfig.mockResolvedValue({
      self_service_enabled: true,
      allow_magpie: true,
      allow_private_magpie_url: false,
      is_admin: false,
      enabled: true,
      has_key: true,
      masked_key: 'hubu-12345...abcd',
      generated_key: 'hubu-1234567890abcdef',
      include_sub2api: true,
      include_disabled: false,
      expose_reset_credits: false,
      hub_path: '/v0/management',
      auth_files_path: '/v0/management/auth-files',
      api_call_path: '/v0/management/api-call',
      supported_platforms: ['codex'],
      magpie_platforms: ['codex', 'claude', 'antigravity'],
      max_magpie_sources: 5,
      cache_ttl_seconds: 30,
      magpie_sources: []
    })

    const wrapper = mount(UserUsageHubView)
    await flushPromises()

    await wrapper.find('[data-testid="user-hub-enabled-toggle"]').setValue(true)
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith({ enabled: true })
    expect(wrapper.find('[data-testid="user-generated-key-banner"]').exists()).toBe(true)
    expect(
      (wrapper.find('[data-testid="user-generated-key-value"]').element as HTMLInputElement).value
    ).toBe('hubu-1234567890abcdef')
  })

  it('adds, tests and saves a custom Magpie source', async () => {
    testMagpieSource.mockResolvedValue({
      ok: true,
      normalized_url: 'https://ellm.zhzy.fun/magpie',
      total_raw_quotas: 3,
      matched_count: 3,
      provider_counts: { codex: 1, claude: 1, antigravity: 1 },
      sample_labels: ['[codex] My Codex']
    })
    updateConfig.mockResolvedValue({
      self_service_enabled: true,
      allow_magpie: true,
      allow_private_magpie_url: false,
      is_admin: false,
      enabled: true,
      has_key: true,
      masked_key: 'hubu-12345...abcd',
      include_sub2api: true,
      include_disabled: false,
      expose_reset_credits: false,
      hub_path: '/v0/management',
      auth_files_path: '/v0/management/auth-files',
      api_call_path: '/v0/management/api-call',
      supported_platforms: ['codex'],
      magpie_platforms: ['codex', 'claude', 'antigravity'],
      max_magpie_sources: 5,
      cache_ttl_seconds: 30,
      magpie_sources: [
        {
          id: 'mp_home',
          name: 'My Magpie',
          enabled: true,
          base_url: 'https://ellm.zhzy.fun/magpie',
          has_api_key: true,
          masked_api_key: 'mgp-1234...9999',
          providers: ['codex', 'claude', 'antigravity']
        }
      ]
    })

    const wrapper = mount(UserUsageHubView)
    await flushPromises()

    await wrapper.find('[data-testid="add-magpie-source-btn"]').trigger('click')
    await wrapper.find('[data-testid="magpie-form-name"]').setValue('My Magpie')
    await wrapper.find('[data-testid="magpie-form-url"]').setValue('https://ellm.zhzy.fun/magpie')
    await wrapper.find('[data-testid="magpie-form-key"]').setValue('mgp-ro-key')

    await wrapper.find('[data-testid="magpie-modal-test-btn"]').trigger('click')
    await flushPromises()

    expect(testMagpieSource).toHaveBeenCalledWith({
      source_id: undefined,
      base_url: 'https://ellm.zhzy.fun/magpie',
      api_key: 'mgp-ro-key',
      providers: ['codex', 'claude', 'antigravity']
    })
    expect(wrapper.find('[data-testid="magpie-test-result"]').exists()).toBe(true)

    await wrapper.find('[data-testid="magpie-modal-save-btn"]').trigger('click')
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith({
      magpie_sources: [
        {
          id: undefined,
          name: 'My Magpie',
          enabled: true,
          base_url: 'https://ellm.zhzy.fun/magpie',
          api_key: 'mgp-ro-key',
          providers: ['codex', 'claude', 'antigravity']
        }
      ]
    })
    expect(wrapper.find('[data-testid="magpie-source-mp_home"]').exists()).toBe(true)
  })
})
