import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

import UsageHubView from '../UsageHubView.vue'

const {
  getConfig,
  updateConfig,
  regenerateKey,
  deleteKey,
  listAccounts,
  refreshAccount,
  refreshAllAccounts,
  previewAuthFiles,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  getConfig: vi.fn(),
  updateConfig: vi.fn(),
  regenerateKey: vi.fn(),
  deleteKey: vi.fn(),
  listAccounts: vi.fn(),
  refreshAccount: vi.fn(),
  refreshAllAccounts: vi.fn(),
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
  adminAPI: {
    usageHub: {
      getConfig,
      updateConfig,
      regenerateKey,
      deleteKey,
      listAccounts,
      refreshAccount,
      refreshAllAccounts,
      previewAuthFiles
    }
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

vi.mock('@/components/auth/TotpStepUpDialog.vue', () => ({
  default: defineComponent({
    name: 'TotpStepUpDialogStub',
    render: () => h('div')
  })
}))

const routerLinkStub = defineComponent({
  name: 'RouterLinkStub',
  props: {
    to: {
      type: [String, Object],
      default: ''
    }
  },
  setup(_, { slots }) {
    return () => h('a', { class: 'router-link-stub' }, slots.default?.())
  }
})

const mountUsageHubView = () =>
  mount(UsageHubView, {
    global: {
      stubs: {
        RouterLink: routerLinkStub,
        'router-link': routerLinkStub
      }
    }
  })

describe('UsageHubView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getConfig.mockResolvedValue({
      enabled: false,
      include_disabled: false,
      self_service_enabled: true,
      allow_magpie: true,
      expose_reset_credits: false,
      active_self_service_users: 2,
      has_key: false,
      masked_key: '',
      hub_path: '/v0/management',
      auth_files_path: '/v0/management/auth-files',
      api_call_path: '/v0/management/api-call',
      supported_platforms: ['codex'],
      allowed_upstream_urls: ['https://chatgpt.com/backend-api/wham/usage'],
      cache_ttl_seconds: 30
    })
    listAccounts.mockResolvedValue({
      summary: {
        total_accounts: 1,
        exposed_accounts: 1,
        healthy_accounts: 1,
        rate_limited_accounts: 0,
        error_accounts: 0,
        disabled_accounts: 0,
        known_5h_count: 1,
        known_7d_count: 1,
        avg_5h_used_percent: 25,
        avg_7d_used_percent: 40,
        total_reset_credits: 2
      },
      accounts: [
        {
          id: 101,
          name: 'Codex Primary',
          email: 'codex@example.com',
          auth_index: 'sub2api-codex-101',
          auth_file_name: 'codex-codex@example.com-101.json',
          chatgpt_account_id: 'acct-101',
          plan_type: 'plus',
          status: 'active',
          schedulable: true,
          exposed_to_hub: true,
          rate_limited: false,
          five_hour: { utilization: 25, remaining_seconds: 7200, resets_at: '2026-11-25T15:00:00Z' },
          seven_day: { utilization: 40, remaining_seconds: 172800, resets_at: '2026-11-28T15:00:00Z' },
          reset_credits: { available_count: 2 }
        }
      ]
    })
  })

  it('loads hub config and displays Codex OAuth accounts', async () => {
    const wrapper = mountUsageHubView()
    await flushPromises()

    expect(getConfig).toHaveBeenCalledTimes(1)
    expect(listAccounts).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="usage-hub-account-101"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Codex Primary')
    expect(wrapper.text()).toContain('sub2api-codex-101')
  })

  it('enables hub and displays auto-generated key once', async () => {
    updateConfig.mockResolvedValue({
      enabled: true,
      include_disabled: false,
      self_service_enabled: true,
      allow_magpie: true,
      expose_reset_credits: false,
      active_self_service_users: 2,
      has_key: true,
      masked_key: 'hub-123456...abcd',
      generated_key: 'hub-1234567890abcdef',
      hub_path: '/v0/management',
      auth_files_path: '/v0/management/auth-files',
      api_call_path: '/v0/management/api-call',
      supported_platforms: ['codex'],
      allowed_upstream_urls: ['https://chatgpt.com/backend-api/wham/usage'],
      cache_ttl_seconds: 30
    })

    const wrapper = mountUsageHubView()
    await flushPromises()

    const checkbox = wrapper.find('[data-testid="hub-enabled-toggle"]')
    await checkbox.setValue(true)
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith({ enabled: true })
    expect(wrapper.find('[data-testid="generated-key-banner"]').exists()).toBe(true)
    expect(
      (wrapper.find('[data-testid="generated-key-value"]').element as HTMLInputElement).value
    ).toBe('hub-1234567890abcdef')
  })

  it('opens auth-files preview modal and triggers batch refresh', async () => {
    previewAuthFiles.mockResolvedValue({
      files: [{ id: 'codex-codex@example.com-101.json', provider: 'codex', auth_index: 'sub2api-codex-101' }]
    })
    refreshAllAccounts.mockResolvedValue({
      refreshed: 1,
      failed: 0,
      overview: await listAccounts()
    })

    const wrapper = mountUsageHubView()
    await flushPromises()

    await wrapper.find('[data-testid="preview-auth-files-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="auth-files-preview-modal"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="auth-files-preview-modal"]').text()).toContain('sub2api-codex-101')

    await wrapper.find('[data-testid="refresh-all-btn"]').trigger('click')
    await flushPromises()
    expect(refreshAllAccounts).toHaveBeenCalledTimes(1)
    expect(showSuccess).toHaveBeenCalled()
  })
})
