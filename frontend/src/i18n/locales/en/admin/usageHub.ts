export default {
  usageHub: {
    title: 'Usage Hub',
    description: 'CLIProxyAPI-compatible site-wide Codex OAuth usage hub for direct T3 Code quota integration',
    statusEnabled: 'Enabled',
    statusDisabled: 'Disabled',
    configTitle: 'T3 Code / CLIProxyAPI Compatibility',
    configSubtitle: 'In T3 Code, go to Settings → Providers → Usage providers → Add hub and enter the Hub URL and Management Key below to monitor all Codex OAuth accounts.',
    enableLabel: 'Enable external Usage Hub (/v0/management/*)',
    includeDisabledLabel: 'Include disabled Codex accounts in Hub auth-files',
    hubUrlLabel: 'Hub Base URL',
    hubUrlHint: 'Use the site root URL; T3 Code automatically queries /v0/management/auth-files and /v0/management/api-call',
    managementKeyLabel: 'Hub Management Key (Read-only)',
    managementKeyNotConfigured: 'Not generated yet (auto-generated when enabling, or generate manually)',
    newKeyBannerTitle: 'Copy and save your new Hub Management Key now',
    newKeyBannerDesc: 'For security, the full key is displayed only once. After closing or refreshing this page, only the masked preview remains.',
    curlExampleLabel: 'Quick Verification Command',
    securityHint: 'Security guard: /v0/management/api-call only allows read-only GET requests to allowlisted official ChatGPT/Codex quota endpoints, with a built-in {seconds}s response cache to prevent upstream rate limits.',
    allowedEndpointsTitle: 'Allowlisted Upstream Read-only Endpoints',
    actions: {
      copy: 'Copy',
      copied: 'Copied to clipboard',
      copyFailed: 'Copy failed, please copy manually',
      generateKey: 'Generate Key',
      regenerateKey: 'Rotate Key',
      deleteKey: 'Delete Key',
      previewAuthFiles: 'Preview auth-files',
      refreshAll: 'Refresh All Quotas',
      refreshing: 'Refreshing...',
      refreshOne: 'Refresh',
      close: 'Close',
      reload: 'Reload'
    },
    confirm: {
      regenerateKey: 'Rotating the key immediately invalidates the previous key. Existing T3 Code hubs will need the new key. Continue?',
      deleteKey: 'Deleting the key also disables external Usage Hub access for T3 Code. Continue?'
    },
    messages: {
      configSaved: 'Usage Hub configuration saved',
      keyGenerated: 'New Hub Management Key generated',
      keyDeleted: 'Hub Management Key deleted and external access disabled',
      accountRefreshed: 'Refreshed quota for {name}',
      batchRefreshed: 'Batch refresh finished: {refreshed} succeeded, {failed} failed'
    },
    summary: {
      totalAccounts: 'Codex OAuth Accounts',
      exposedHint: '{count} exposed to Hub',
      statusDistribution: 'Account Health',
      healthy: 'Healthy',
      rateLimited: 'Rate-limited',
      error: 'Error',
      disabled: 'Disabled',
      avg5h: 'Avg 5h Window Usage',
      avg7d: 'Avg 7d Window Usage',
      knownCount: '{count} sampled',
      resetCredits: 'Available Reset Credits',
      resetCreditsHint: 'From latest /wham/usage snapshots'
    },
    accounts: {
      title: 'Site-wide Codex OAuth Quota Overview',
      subtitle: 'Tracks non-shadow OpenAI OAuth accounts using each account proxy and impersonated upstream client',
      searchPlaceholder: 'Search by name, email, auth_index, or ChatGPT Account ID...',
      filterAll: 'All',
      filterHealthy: 'Healthy',
      filterRateLimited: 'Rate-limited',
      filterError: 'Error',
      filterDisabled: 'Disabled',
      empty: 'No matching Codex OAuth accounts found',
      columns: {
        account: 'Account / Identity',
        planStatus: 'Plan / Status',
        fiveHour: '5h Window',
        sevenDay: '7d Window',
        credits: 'Reset Credits / Balance',
        updatedAt: 'Last Observed',
        actions: 'Actions'
      },
      exposedBadge: 'Exposed',
      hiddenBadge: 'Hidden',
      schedulablePaused: 'Paused',
      unknownWindow: 'No data',
      resetIn: 'Resets in ~{time}',
      resetReady: 'Reset due',
      resetCreditsCount: '{count} reset credits',
      unlimitedCredits: 'Unlimited',
      neverUpdated: 'Never refreshed'
    },
    previewModal: {
      title: 'GET /v0/management/auth-files Payload Preview',
      subtitle: 'Sanitized JSON returned to T3 Code when enumerating accounts (no access_token or refresh_token exposed)',
      fileCount: '{count} auth file entries'
    }
  }
}
