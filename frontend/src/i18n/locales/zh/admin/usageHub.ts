export default {
  usageHub: {
    title: '用量 Hub',
    description: '兼容 CLIProxyAPI 协议的全站 Codex OAuth 用量中心，可直接接入 T3 Code 实时查看账号池额度',
    statusEnabled: '已启用',
    statusDisabled: '未启用',
    configTitle: 'T3 Code / CLIProxyAPI 兼容接入',
    configSubtitle: '在 T3 Code 中进入 Settings → Providers → Usage providers → Add hub，填入下方 Hub 地址与管理密钥即可自动拉取全站 Codex OAuth 账号池额度。',
    enableLabel: '启用对外用量 Hub（/v0/management/*）',
    includeDisabledLabel: '在 Hub 列表中包含已禁用的 Codex 账号',
    hubUrlLabel: 'Hub 地址（Base URL）',
    hubUrlHint: '直接填写当前站点根地址；T3 Code 会自动请求 /v0/management/auth-files 与 /v0/management/api-call',
    managementKeyLabel: 'Hub 管理密钥（只读）',
    managementKeyNotConfigured: '尚未生成密钥（启用 Hub 时会自动生成，也可手动生成）',
    newKeyBannerTitle: '请立即复制并保存新的 Hub 管理密钥',
    newKeyBannerDesc: '出于安全考虑，明文密钥仅展示这一次；关闭或刷新页面后只能查看脱敏摘要或重新轮换。',
    curlExampleLabel: '快速验证命令',
    securityHint: '安全防护：/v0/management/api-call 仅允许 GET 只读请求且强制限定在官方 ChatGPT/Codex 额度白名单端点内，已内置 {seconds} 秒响应缓存以防止高频刷新触发上游限流。',
    allowedEndpointsTitle: '已放行的上游只读白名单端点',
    actions: {
      copy: '复制',
      copied: '已复制到剪贴板',
      copyFailed: '复制失败，请手动复制',
      generateKey: '生成密钥',
      regenerateKey: '轮换密钥',
      deleteKey: '删除密钥',
      previewAuthFiles: '预览 auth-files',
      refreshAll: '批量刷新额度',
      refreshing: '刷新中...',
      refreshOne: '刷新',
      close: '关闭',
      reload: '重新加载'
    },
    confirm: {
      regenerateKey: '轮换密钥后，旧密钥将立即失效，已在 T3 Code 中配置的 Hub 需要更新为新密钥。确定继续吗？',
      deleteKey: '删除密钥将同时关闭对外用量 Hub，T3 Code 将无法继续读取额度。确定继续吗？'
    },
    messages: {
      configSaved: '用量 Hub 配置已保存',
      keyGenerated: '已生成新的 Hub 管理密钥',
      keyDeleted: '已删除 Hub 管理密钥并关闭对外访问',
      accountRefreshed: '账号 {name} 额度已刷新',
      batchRefreshed: '批量刷新完成：成功 {refreshed} 个，失败 {failed} 个'
    },
    summary: {
      totalAccounts: 'Codex OAuth 账号',
      exposedHint: '对外暴露 {count} 个',
      statusDistribution: '账号健康分布',
      healthy: '正常',
      rateLimited: '限流中',
      error: '异常',
      disabled: '禁用',
      avg5h: '5 小时窗口均值',
      avg7d: '7 天窗口均值',
      knownCount: '已采样 {count} 个账号',
      resetCredits: '可用重置卡总数',
      resetCreditsHint: '来自最近一次 /wham/usage 观测'
    },
    accounts: {
      title: '全站 Codex OAuth 账号额度总览',
      subtitle: '仅纳管非影子 OpenAI OAuth 账号，自动复用账号代理与官方 TLS 指纹客户端刷新额度',
      searchPlaceholder: '搜索账号名称、邮箱、auth_index 或 ChatGPT Account ID...',
      filterAll: '全部',
      filterHealthy: '正常',
      filterRateLimited: '限流',
      filterError: '异常',
      filterDisabled: '禁用',
      empty: '暂无匹配的 Codex OAuth 账号',
      columns: {
        account: '账号 / 身份',
        planStatus: '套餐 / 状态',
        fiveHour: '5h 窗口',
        sevenDay: '7d 窗口',
        credits: '重置卡 / 余额',
        updatedAt: '最近观测',
        actions: '操作'
      },
      exposedBadge: '已暴露给 Hub',
      hiddenBadge: '未暴露',
      schedulablePaused: '已停调',
      unknownWindow: '暂无观测',
      resetIn: '约 {time} 后重置',
      resetReady: '已到重置点',
      resetCreditsCount: '{count} 张重置卡',
      unlimitedCredits: '不限额',
      neverUpdated: '未刷新'
    },
    previewModal: {
      title: 'GET /v0/management/auth-files 响应预览',
      subtitle: '以下为当前 T3 Code 拉取账号池时将实际收到的脱敏 JSON（不含 access_token / refresh_token）',
      fileCount: '共 {count} 条账号记录'
    }
  }
}
