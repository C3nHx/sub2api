<template>
  <AppLayout>
    <div class="space-y-6">
      <!-- 顶部：CLIProxyAPI 兼容接入配置卡片 -->
      <div class="card p-6">
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div class="space-y-1">
            <div class="flex flex-wrap items-center gap-2.5">
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('admin.usageHub.configTitle') }}
              </h2>
              <span
                class="inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium"
                :class="
                  config.enabled
                    ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
                    : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
                "
              >
                {{ config.enabled ? t('admin.usageHub.statusEnabled') : t('admin.usageHub.statusDisabled') }}
              </span>
              <span class="inline-flex items-center rounded-full bg-primary-50 px-2.5 py-0.5 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
                {{ t('admin.usageHub.activeSelfServiceUsersBadge', { count: config.active_self_service_users || 0 }) }}
              </span>
            </div>
            <p class="text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.usageHub.configSubtitle') }}
            </p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <router-link to="/usage-hub" class="btn btn-secondary btn-sm">
              {{ t('admin.usageHub.openPersonalHub') }}
            </router-link>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="previewLoading"
              data-testid="preview-auth-files-btn"
              @click="openPreviewModal"
            >
              {{ previewLoading ? t('common.loading') : t('admin.usageHub.actions.previewAuthFiles') }}
            </button>
            <button
              type="button"
              class="btn btn-primary btn-sm"
              :disabled="refreshingAll || loading"
              data-testid="refresh-all-btn"
              @click="handleRefreshAll"
            >
              {{ refreshingAll ? t('admin.usageHub.actions.refreshing') : t('admin.usageHub.actions.refreshAll') }}
            </button>
          </div>
        </div>

        <!-- 开关区 -->
        <div class="mt-5 flex flex-wrap items-center gap-x-6 gap-y-3 border-t border-gray-100 pt-4 dark:border-dark-700">
          <label class="inline-flex cursor-pointer items-center gap-2.5 text-sm font-medium text-gray-800 dark:text-gray-200">
            <input
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="config.self_service_enabled"
              :disabled="savingConfig"
              data-testid="hub-self-service-toggle"
              @change="handleUpdateConfigFlag({ self_service_enabled: ($event.target as HTMLInputElement).checked })"
            />
            <span>{{ t('admin.usageHub.selfServiceEnabledLabel') }}</span>
          </label>
          <label class="inline-flex cursor-pointer items-center gap-2.5 text-sm text-gray-700 dark:text-gray-300">
            <input
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="config.allow_magpie"
              :disabled="savingConfig"
              data-testid="hub-allow-magpie-toggle"
              @change="handleUpdateConfigFlag({ allow_magpie: ($event.target as HTMLInputElement).checked })"
            />
            <span>{{ t('admin.usageHub.allowMagpieLabel') }}</span>
          </label>
          <label class="inline-flex cursor-pointer items-center gap-2.5 text-sm font-medium text-gray-800 dark:text-gray-200">
            <input
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="config.enabled"
              :disabled="savingConfig"
              data-testid="hub-enabled-toggle"
              @change="handleToggleEnabled(($event.target as HTMLInputElement).checked)"
            />
            <span>{{ t('admin.usageHub.enableLabel') }}</span>
          </label>
          <label class="inline-flex cursor-pointer items-center gap-2.5 text-sm text-gray-700 dark:text-gray-300">
            <input
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="config.include_disabled"
              :disabled="savingConfig"
              data-testid="hub-include-disabled-toggle"
              @change="handleToggleIncludeDisabled(($event.target as HTMLInputElement).checked)"
            />
            <span>{{ t('admin.usageHub.includeDisabledLabel') }}</span>
          </label>
          <label class="inline-flex cursor-pointer items-center gap-2.5 text-sm text-gray-700 dark:text-gray-300">
            <input
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="config.expose_reset_credits"
              :disabled="savingConfig"
              data-testid="hub-expose-reset-toggle"
              @change="handleUpdateConfigFlag({ expose_reset_credits: ($event.target as HTMLInputElement).checked })"
            />
            <span>{{ t('admin.usageHub.exposeResetCreditsLabel') }}</span>
          </label>
        </div>

        <!-- Hub 地址与 Management Key 卡片 -->
        <div class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2">
          <!-- Hub Base URL -->
          <div class="rounded-xl border border-gray-200 bg-gray-50/70 p-4 dark:border-dark-700 dark:bg-dark-900/50">
            <div class="mb-1.5 text-xs font-medium text-gray-600 dark:text-gray-400">
              {{ t('admin.usageHub.hubUrlLabel') }}
            </div>
            <div class="flex items-center gap-2">
              <input
                :value="hubBaseUrl"
                readonly
                class="input flex-1 font-mono text-xs"
                data-testid="hub-base-url-input"
              />
              <button
                type="button"
                class="btn btn-secondary btn-sm shrink-0"
                @click="copyText(hubBaseUrl)"
              >
                {{ t('admin.usageHub.actions.copy') }}
              </button>
            </div>
            <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.usageHub.hubUrlHint') }}
            </p>
          </div>

          <!-- Hub Management Key -->
          <div class="rounded-xl border border-gray-200 bg-gray-50/70 p-4 dark:border-dark-700 dark:bg-dark-900/50">
            <div class="mb-1.5 text-xs font-medium text-gray-600 dark:text-gray-400">
              {{ t('admin.usageHub.managementKeyLabel') }}
            </div>
            <div class="flex flex-wrap items-center gap-2">
              <div
                class="flex-1 truncate rounded-lg border border-gray-200 bg-white px-3 py-2 font-mono text-xs text-gray-700 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-200"
                data-testid="hub-masked-key"
              >
                {{ config.has_key ? config.masked_key : t('admin.usageHub.managementKeyNotConfigured') }}
              </div>
              <button
                type="button"
                class="btn btn-secondary btn-sm shrink-0"
                :disabled="keyActionLoading"
                data-testid="regenerate-key-btn"
                @click="handleRegenerateKey"
              >
                {{
                  config.has_key
                    ? t('admin.usageHub.actions.regenerateKey')
                    : t('admin.usageHub.actions.generateKey')
                }}
              </button>
              <button
                v-if="config.has_key"
                type="button"
                class="btn btn-danger btn-sm shrink-0"
                :disabled="keyActionLoading"
                data-testid="delete-key-btn"
                @click="handleDeleteKey"
              >
                {{ t('admin.usageHub.actions.deleteKey') }}
              </button>
            </div>
            <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.usageHub.securityHint', { seconds: config.cache_ttl_seconds || 30 }) }}
            </p>
          </div>
        </div>

        <!-- 新生成明文 Key 一次性展示横幅 -->
        <div
          v-if="newlyGeneratedKey"
          class="mt-4 rounded-xl border border-amber-300 bg-amber-50 p-4 dark:border-amber-700/60 dark:bg-amber-950/30"
          data-testid="generated-key-banner"
        >
          <div class="flex flex-wrap items-start justify-between gap-2">
            <div>
              <h3 class="text-sm font-semibold text-amber-900 dark:text-amber-200">
                {{ t('admin.usageHub.newKeyBannerTitle') }}
              </h3>
              <p class="mt-0.5 text-xs text-amber-800/90 dark:text-amber-300/90">
                {{ t('admin.usageHub.newKeyBannerDesc') }}
              </p>
            </div>
            <button
              type="button"
              class="text-xs font-medium text-amber-700 hover:underline dark:text-amber-300"
              @click="newlyGeneratedKey = ''"
            >
              {{ t('admin.usageHub.actions.close') }}
            </button>
          </div>
          <div class="mt-3 flex items-center gap-2">
            <input
              :value="newlyGeneratedKey"
              readonly
              class="input flex-1 font-mono text-xs"
              data-testid="generated-key-value"
            />
            <button
              type="button"
              class="btn btn-primary btn-sm shrink-0"
              @click="copyText(newlyGeneratedKey)"
            >
              {{ t('admin.usageHub.actions.copy') }}
            </button>
          </div>
          <div class="mt-3">
            <div class="mb-1 text-xs font-medium text-amber-900/90 dark:text-amber-200/90">
              {{ t('admin.usageHub.curlExampleLabel') }}
            </div>
            <div class="flex items-center gap-2">
              <code class="block flex-1 overflow-x-auto rounded-lg bg-gray-900 px-3 py-2 font-mono text-xs text-gray-100">
                {{ curlExampleCommand }}
              </code>
              <button
                type="button"
                class="btn btn-secondary btn-sm shrink-0"
                @click="copyText(curlExampleCommand)"
              >
                {{ t('admin.usageHub.actions.copy') }}
              </button>
            </div>
          </div>
        </div>

        <!-- 白名单端点提示 -->
        <div
          v-if="config.allowed_upstream_urls.length > 0"
          class="mt-4 flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400"
        >
          <span class="font-medium text-gray-600 dark:text-gray-300">
            {{ t('admin.usageHub.allowedEndpointsTitle') }}:
          </span>
          <code
            v-for="endpoint in config.allowed_upstream_urls"
            :key="endpoint"
            class="rounded bg-gray-100 px-2 py-0.5 font-mono text-[11px] text-gray-700 dark:bg-dark-800 dark:text-gray-300"
          >
            GET {{ endpoint }}
          </code>
        </div>
      </div>

      <!-- 中部：全站 Codex OAuth 聚合 KPI 卡片 -->
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <!-- 总账号与暴露数 -->
        <div class="card p-5">
          <div class="text-xs font-medium text-gray-500 dark:text-gray-400">
            {{ t('admin.usageHub.summary.totalAccounts') }}
          </div>
          <div class="mt-2 flex items-baseline gap-2">
            <span class="text-2xl font-bold text-gray-900 dark:text-white">
              {{ overview.summary.total_accounts }}
            </span>
            <span class="text-xs text-primary-600 dark:text-primary-400">
              {{ t('admin.usageHub.summary.exposedHint', { count: overview.summary.exposed_accounts }) }}
            </span>
          </div>
          <div class="mt-2 flex flex-wrap gap-2 text-xs text-gray-500 dark:text-gray-400">
            <span class="text-emerald-600 dark:text-emerald-400">
              {{ t('admin.usageHub.summary.healthy') }} {{ overview.summary.healthy_accounts }}
            </span>
            <span class="text-amber-600 dark:text-amber-400">
              {{ t('admin.usageHub.summary.rateLimited') }} {{ overview.summary.rate_limited_accounts }}
            </span>
            <span class="text-rose-600 dark:text-rose-400">
              {{ t('admin.usageHub.summary.error') }} {{ overview.summary.error_accounts }}
            </span>
            <span>
              {{ t('admin.usageHub.summary.disabled') }} {{ overview.summary.disabled_accounts }}
            </span>
          </div>
        </div>

        <!-- 5 小时窗口均值 -->
        <div class="card p-5">
          <div class="flex items-center justify-between text-xs font-medium text-gray-500 dark:text-gray-400">
            <span>{{ t('admin.usageHub.summary.avg5h') }}</span>
            <span>{{ t('admin.usageHub.summary.knownCount', { count: overview.summary.known_5h_count }) }}</span>
          </div>
          <div class="mt-2 text-2xl font-bold text-gray-900 dark:text-white">
            {{ formatPercent(overview.summary.avg_5h_used_percent) }}
          </div>
          <div class="mt-3 h-2 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
            <div
              class="h-full rounded-full transition-all"
              :class="progressBarColor(overview.summary.avg_5h_used_percent)"
              :style="{ width: `${clampPercent(overview.summary.avg_5h_used_percent)}%` }"
            />
          </div>
        </div>

        <!-- 7 天窗口均值 -->
        <div class="card p-5">
          <div class="flex items-center justify-between text-xs font-medium text-gray-500 dark:text-gray-400">
            <span>{{ t('admin.usageHub.summary.avg7d') }}</span>
            <span>{{ t('admin.usageHub.summary.knownCount', { count: overview.summary.known_7d_count }) }}</span>
          </div>
          <div class="mt-2 text-2xl font-bold text-gray-900 dark:text-white">
            {{ formatPercent(overview.summary.avg_7d_used_percent) }}
          </div>
          <div class="mt-3 h-2 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
            <div
              class="h-full rounded-full transition-all"
              :class="progressBarColor(overview.summary.avg_7d_used_percent)"
              :style="{ width: `${clampPercent(overview.summary.avg_7d_used_percent)}%` }"
            />
          </div>
        </div>

        <!-- 重置卡总数 -->
        <div class="card p-5">
          <div class="text-xs font-medium text-gray-500 dark:text-gray-400">
            {{ t('admin.usageHub.summary.resetCredits') }}
          </div>
          <div class="mt-2 text-2xl font-bold text-gray-900 dark:text-white">
            {{ overview.summary.total_reset_credits }}
          </div>
          <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.usageHub.summary.resetCreditsHint') }}
          </p>
        </div>
      </div>

      <!-- 底部：全站 Codex OAuth 账号列表 -->
      <div class="card overflow-hidden">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 p-5 dark:border-dark-700">
          <div>
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">
              {{ t('admin.usageHub.accounts.title') }}
            </h3>
            <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.usageHub.accounts.subtitle') }}
            </p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <input
              v-model.trim="searchQuery"
              type="text"
              class="input w-64 text-sm"
              :placeholder="t('admin.usageHub.accounts.searchPlaceholder')"
              data-testid="accounts-search-input"
            />
            <div class="inline-flex rounded-lg border border-gray-200 p-0.5 dark:border-dark-700">
              <button
                v-for="tab in statusTabs"
                :key="tab.value"
                type="button"
                class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
                :class="
                  statusFilter === tab.value
                    ? 'bg-primary-600 text-white'
                    : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'
                "
                @click="statusFilter = tab.value"
              >
                {{ tab.label }}
              </button>
            </div>
          </div>
        </div>

        <div v-if="loading" class="p-10 text-center text-sm text-gray-500 dark:text-gray-400">
          {{ t('common.loading') }}
        </div>

        <div
          v-else-if="filteredAccounts.length === 0"
          class="p-10 text-center text-sm text-gray-500 dark:text-gray-400"
        >
          {{ t('admin.usageHub.accounts.empty') }}
        </div>

        <div v-else class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
            <thead class="bg-gray-50 dark:bg-dark-900/60">
              <tr>
                <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  {{ t('admin.usageHub.accounts.columns.account') }}
                </th>
                <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  {{ t('admin.usageHub.accounts.columns.planStatus') }}
                </th>
                <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  {{ t('admin.usageHub.accounts.columns.fiveHour') }}
                </th>
                <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  {{ t('admin.usageHub.accounts.columns.sevenDay') }}
                </th>
                <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  {{ t('admin.usageHub.accounts.columns.credits') }}
                </th>
                <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  {{ t('admin.usageHub.accounts.columns.updatedAt') }}
                </th>
                <th class="px-4 py-3 text-right text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  {{ t('admin.usageHub.accounts.columns.actions') }}
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-800 dark:bg-dark-800/40">
              <tr
                v-for="item in filteredAccounts"
                :key="item.id"
                class="hover:bg-gray-50/70 dark:hover:bg-dark-700/30"
                :data-testid="`usage-hub-account-${item.id}`"
              >
                <!-- 账号与身份 -->
                <td class="px-4 py-3.5 align-top">
                  <div class="font-medium text-gray-900 dark:text-white">{{ item.name }}</div>
                  <div v-if="item.email" class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ item.email }}
                  </div>
                  <div class="mt-1 flex flex-wrap items-center gap-1.5 font-mono text-[11px] text-gray-400 dark:text-gray-500">
                    <span>{{ item.auth_index }}</span>
                    <span v-if="item.chatgpt_account_id">· {{ item.chatgpt_account_id }}</span>
                  </div>
                </td>

                <!-- 套餐与状态 -->
                <td class="px-4 py-3.5 align-top">
                  <div class="flex flex-wrap items-center gap-1.5">
                    <span
                      v-if="item.plan_type"
                      class="inline-flex rounded bg-indigo-100 px-2 py-0.5 text-[11px] font-medium uppercase text-indigo-700 dark:bg-indigo-900/40 dark:text-indigo-300"
                    >
                      {{ item.plan_type }}
                    </span>
                    <span
                      class="inline-flex rounded-full px-2 py-0.5 text-[11px] font-medium"
                      :class="accountStatusBadgeClass(item)"
                    >
                      {{ accountStatusLabel(item) }}
                    </span>
                  </div>
                  <div class="mt-1.5 flex flex-wrap items-center gap-1.5">
                    <span
                      class="inline-flex rounded px-1.5 py-0.5 text-[10px] font-medium"
                      :class="
                        item.exposed_to_hub
                          ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                          : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-gray-400'
                      "
                    >
                      {{
                        item.exposed_to_hub
                          ? t('admin.usageHub.accounts.exposedBadge')
                          : t('admin.usageHub.accounts.hiddenBadge')
                      }}
                    </span>
                    <span
                      v-if="!item.schedulable && item.status === 'active'"
                      class="inline-flex rounded bg-amber-50 px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
                    >
                      {{ t('admin.usageHub.accounts.schedulablePaused') }}
                    </span>
                  </div>
                  <div
                    v-if="item.error_message"
                    class="mt-1 max-w-xs truncate text-[11px] text-rose-600 dark:text-rose-400"
                    :title="item.error_message"
                  >
                    {{ item.error_message }}
                  </div>
                </td>

                <!-- 5h 窗口 -->
                <td class="w-44 px-4 py-3.5 align-top">
                  <template v-if="item.five_hour && !item.five_hour.utilization_unknown">
                    <div class="flex items-center justify-between text-xs font-medium">
                      <span class="text-gray-800 dark:text-gray-200">
                        {{ formatPercent(item.five_hour.utilization) }}
                      </span>
                      <span class="text-[11px] text-gray-400">
                        {{ formatResetCountdown(item.five_hour.remaining_seconds, item.five_hour.resets_at) }}
                      </span>
                    </div>
                    <div class="mt-1.5 h-2 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
                      <div
                        class="h-full rounded-full transition-all"
                        :class="progressBarColor(item.five_hour.utilization)"
                        :style="{ width: `${clampPercent(item.five_hour.utilization)}%` }"
                      />
                    </div>
                  </template>
                  <span v-else class="text-xs text-gray-400">
                    {{ t('admin.usageHub.accounts.unknownWindow') }}
                  </span>
                </td>

                <!-- 7d 窗口 -->
                <td class="w-44 px-4 py-3.5 align-top">
                  <template v-if="item.seven_day && !item.seven_day.utilization_unknown">
                    <div class="flex items-center justify-between text-xs font-medium">
                      <span class="text-gray-800 dark:text-gray-200">
                        {{ formatPercent(item.seven_day.utilization) }}
                      </span>
                      <span class="text-[11px] text-gray-400">
                        {{ formatResetCountdown(item.seven_day.remaining_seconds, item.seven_day.resets_at) }}
                      </span>
                    </div>
                    <div class="mt-1.5 h-2 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
                      <div
                        class="h-full rounded-full transition-all"
                        :class="progressBarColor(item.seven_day.utilization)"
                        :style="{ width: `${clampPercent(item.seven_day.utilization)}%` }"
                      />
                    </div>
                  </template>
                  <span v-else class="text-xs text-gray-400">
                    {{ t('admin.usageHub.accounts.unknownWindow') }}
                  </span>
                </td>

                <!-- 重置卡 / Credits -->
                <td class="px-4 py-3.5 align-top text-xs text-gray-700 dark:text-gray-300">
                  <div v-if="item.reset_credits">
                    {{ t('admin.usageHub.accounts.resetCreditsCount', { count: item.reset_credits.available_count }) }}
                  </div>
                  <div v-if="item.credits" class="mt-0.5 text-[11px] text-gray-500 dark:text-gray-400">
                    <template v-if="item.credits.unlimited">
                      {{ t('admin.usageHub.accounts.unlimitedCredits') }}
                    </template>
                    <template v-else-if="item.credits.balance !== null">
                      ${{ item.credits.balance }}
                    </template>
                  </div>
                  <span v-if="!item.reset_credits && !item.credits" class="text-gray-400">-</span>
                </td>

                <!-- 最近观测时间 -->
                <td class="px-4 py-3.5 align-top text-xs text-gray-500 dark:text-gray-400">
                  {{ item.updated_at ? formatDateTime(item.updated_at) : t('admin.usageHub.accounts.neverUpdated') }}
                </td>

                <!-- 单账号刷新 -->
                <td class="px-4 py-3.5 text-right align-top">
                  <button
                    type="button"
                    class="btn btn-secondary btn-xs"
                    :disabled="refreshingAccountIds.has(item.id) || refreshingAll"
                    :data-testid="`refresh-account-${item.id}`"
                    @click="handleRefreshAccount(item)"
                  >
                    {{
                      refreshingAccountIds.has(item.id)
                        ? t('admin.usageHub.actions.refreshing')
                        : t('admin.usageHub.actions.refreshOne')
                    }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- auth-files 载荷预览弹窗 -->
    <div
      v-if="previewModalOpen"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      data-testid="auth-files-preview-modal"
      @click.self="previewModalOpen = false"
    >
      <div class="flex max-h-[85vh] w-full max-w-3xl flex-col overflow-hidden rounded-2xl bg-white shadow-2xl dark:bg-dark-800">
        <div class="flex items-start justify-between gap-4 border-b border-gray-200 px-6 py-4 dark:border-dark-700">
          <div>
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">
              {{ t('admin.usageHub.previewModal.title') }}
            </h3>
            <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.usageHub.previewModal.subtitle') }}
            </p>
          </div>
          <button
            type="button"
            class="btn btn-secondary btn-xs"
            @click="previewModalOpen = false"
          >
            {{ t('admin.usageHub.actions.close') }}
          </button>
        </div>
        <div class="flex items-center justify-between bg-gray-50 px-6 py-2 text-xs text-gray-600 dark:bg-dark-900/60 dark:text-gray-300">
          <span>{{ t('admin.usageHub.previewModal.fileCount', { count: previewFileCount }) }}</span>
          <button type="button" class="text-primary-600 hover:underline dark:text-primary-400" @click="copyText(previewJSON)">
            {{ t('admin.usageHub.actions.copy') }}
          </button>
        </div>
        <pre class="flex-1 overflow-auto bg-gray-950 p-5 font-mono text-xs leading-relaxed text-emerald-300">{{ previewJSON }}</pre>
      </div>
    </div>

    <!-- Step-up 2FA 二次验证弹窗 -->
    <TotpStepUpDialog :controller="hubStepUp" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { adminAPI } from '@/api'
import type {
  UpdateUsageHubConfigPayload,
  UsageHubAccountItem,
  UsageHubConfig,
  UsageHubOverviewResponse
} from '@/api/admin/usageHub'
import { useAppStore } from '@/stores'
import {
  isStepUpBlocked,
  isStepUpCancelled,
  stepUpBlockReason,
  useStepUp
} from '@/composables/useStepUp'

type StatusFilter = 'all' | 'healthy' | 'rate_limited' | 'error' | 'disabled'

const { t } = useI18n()
const appStore = useAppStore()
const hubStepUp = useStepUp()

const loading = ref(false)
const savingConfig = ref(false)
const keyActionLoading = ref(false)
const refreshingAll = ref(false)
const refreshingAccountIds = ref<Set<number>>(new Set())

const config = ref<UsageHubConfig>({
  enabled: false,
  include_disabled: false,
  self_service_enabled: true,
  allow_magpie: true,
  expose_reset_credits: false,
  active_self_service_users: 0,
  has_key: false,
  masked_key: '',
  hub_path: '/v0/management',
  auth_files_path: '/v0/management/auth-files',
  api_call_path: '/v0/management/api-call',
  supported_platforms: ['codex'],
  allowed_upstream_urls: [],
  cache_ttl_seconds: 30
})

const overview = ref<UsageHubOverviewResponse>({
  summary: {
    total_accounts: 0,
    exposed_accounts: 0,
    healthy_accounts: 0,
    rate_limited_accounts: 0,
    error_accounts: 0,
    disabled_accounts: 0,
    known_5h_count: 0,
    known_7d_count: 0,
    avg_5h_used_percent: null,
    avg_7d_used_percent: null,
    total_reset_credits: 0
  },
  accounts: []
})

const newlyGeneratedKey = ref('')
const searchQuery = ref('')
const statusFilter = ref<StatusFilter>('all')

const previewModalOpen = ref(false)
const previewLoading = ref(false)
const previewJSON = ref('')
const previewFileCount = ref(0)

const hubBaseUrl = computed(() => {
  if (typeof window !== 'undefined' && window.location?.origin) {
    return window.location.origin
  }
  return ''
})

const curlExampleCommand = computed(() => {
  const base = hubBaseUrl.value || 'https://your-sub2api-host'
  const key = newlyGeneratedKey.value || '<YOUR_HUB_KEY>'
  return `curl -s "${base}${config.value.auth_files_path}" -H "Authorization: Bearer ${key}"`
})

const statusTabs = computed<Array<{ value: StatusFilter; label: string }>>(() => [
  { value: 'all', label: t('admin.usageHub.accounts.filterAll') },
  { value: 'healthy', label: t('admin.usageHub.accounts.filterHealthy') },
  { value: 'rate_limited', label: t('admin.usageHub.accounts.filterRateLimited') },
  { value: 'error', label: t('admin.usageHub.accounts.filterError') },
  { value: 'disabled', label: t('admin.usageHub.accounts.filterDisabled') }
])

const filteredAccounts = computed(() => {
  const kw = searchQuery.value.toLowerCase()
  return overview.value.accounts.filter((item) => {
    if (statusFilter.value !== 'all') {
      const bucket = classifyAccount(item)
      if (bucket !== statusFilter.value) return false
    }
    if (!kw) return true
    return (
      item.name.toLowerCase().includes(kw) ||
      (item.email || '').toLowerCase().includes(kw) ||
      item.auth_index.toLowerCase().includes(kw) ||
      (item.chatgpt_account_id || '').toLowerCase().includes(kw)
    )
  })
})

function classifyAccount(item: UsageHubAccountItem): Exclude<StatusFilter, 'all'> {
  if (item.status === 'disabled') return 'disabled'
  if (item.status === 'error') return 'error'
  if (item.rate_limited) return 'rate_limited'
  return 'healthy'
}

function accountStatusLabel(item: UsageHubAccountItem): string {
  switch (classifyAccount(item)) {
    case 'disabled':
      return t('admin.usageHub.summary.disabled')
    case 'error':
      return t('admin.usageHub.summary.error')
    case 'rate_limited':
      return t('admin.usageHub.summary.rateLimited')
    default:
      return t('admin.usageHub.summary.healthy')
  }
}

function accountStatusBadgeClass(item: UsageHubAccountItem): string {
  switch (classifyAccount(item)) {
    case 'disabled':
      return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
    case 'error':
      return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300'
    case 'rate_limited':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
    default:
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
  }
}

function clampPercent(val?: number | null): number {
  if (typeof val !== 'number' || Number.isNaN(val)) return 0
  return Math.max(0, Math.min(100, val))
}

function formatPercent(val?: number | null): string {
  if (typeof val !== 'number' || Number.isNaN(val)) return '-'
  return `${Math.round(val * 10) / 10}%`
}

function progressBarColor(val?: number | null): string {
  const pct = clampPercent(val)
  if (pct >= 90) return 'bg-rose-500'
  if (pct >= 70) return 'bg-amber-500'
  return 'bg-emerald-500'
}

function formatResetCountdown(remainingSeconds: number, resetsAt?: string | null): string {
  if (!resetsAt && remainingSeconds <= 0) return ''
  if (remainingSeconds <= 0) return t('admin.usageHub.accounts.resetReady')
  const days = Math.floor(remainingSeconds / 86400)
  const hours = Math.floor((remainingSeconds % 86400) / 3600)
  const minutes = Math.floor((remainingSeconds % 3600) / 60)
  let timeText = ''
  if (days > 0) {
    timeText = `${days}d ${hours}h`
  } else if (hours > 0) {
    timeText = `${hours}h ${minutes}m`
  } else {
    timeText = `${Math.max(1, minutes)}m`
  }
  return t('admin.usageHub.accounts.resetIn', { time: timeText })
}

function formatDateTime(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return date.toLocaleString()
}

function reportStepUpBlocked(error: unknown): boolean {
  if (!isStepUpBlocked(error)) return false
  appStore.showError(
    stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
      ? t('stepUp.adminApiKeyForbidden')
      : t('stepUp.notEnabled')
  )
  return true
}

function extractErrorMessage(error: unknown): string {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string') {
    return error.message
  }
  return t('errors.networkError')
}

async function copyText(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text)
    appStore.showSuccess(t('admin.usageHub.actions.copied'))
  } catch {
    appStore.showError(t('admin.usageHub.actions.copyFailed'))
  }
}

async function loadAll(): Promise<void> {
  loading.value = true
  try {
    const [cfg, list] = await Promise.all([
      adminAPI.usageHub.getConfig(),
      adminAPI.usageHub.listAccounts()
    ])
    config.value = cfg
    overview.value = list
  } catch (error) {
    appStore.showError(extractErrorMessage(error))
  } finally {
    loading.value = false
  }
}

async function handleToggleEnabled(nextValue: boolean): Promise<void> {
  savingConfig.value = true
  try {
    const updated = await adminAPI.usageHub.updateConfig({ enabled: nextValue })
    config.value = updated
    if (updated.generated_key) {
      newlyGeneratedKey.value = updated.generated_key
    }
    appStore.showSuccess(t('admin.usageHub.messages.configSaved'))
  } catch (error) {
    appStore.showError(extractErrorMessage(error))
  } finally {
    savingConfig.value = false
  }
}

async function handleToggleIncludeDisabled(nextValue: boolean): Promise<void> {
  savingConfig.value = true
  try {
    const updated = await adminAPI.usageHub.updateConfig({ include_disabled: nextValue })
    config.value = updated
    overview.value = await adminAPI.usageHub.listAccounts()
    appStore.showSuccess(t('admin.usageHub.messages.configSaved'))
  } catch (error) {
    appStore.showError(extractErrorMessage(error))
  } finally {
    savingConfig.value = false
  }
}

async function handleUpdateConfigFlag(payload: UpdateUsageHubConfigPayload): Promise<void> {
  savingConfig.value = true
  try {
    const updated = await adminAPI.usageHub.updateConfig(payload)
    config.value = updated
    appStore.showSuccess(t('admin.usageHub.messages.configSaved'))
  } catch (error) {
    appStore.showError(extractErrorMessage(error))
  } finally {
    savingConfig.value = false
  }
}

async function handleRegenerateKey(): Promise<void> {
  if (config.value.has_key && !window.confirm(t('admin.usageHub.confirm.regenerateKey'))) {
    return
  }
  keyActionLoading.value = true
  try {
    const result = await hubStepUp.run(() => adminAPI.usageHub.regenerateKey())
    newlyGeneratedKey.value = result.key
    config.value = {
      ...config.value,
      has_key: true,
      masked_key: result.masked_key
    }
    appStore.showSuccess(t('admin.usageHub.messages.keyGenerated'))
  } catch (error) {
    if (isStepUpCancelled(error) || reportStepUpBlocked(error)) return
    appStore.showError(extractErrorMessage(error))
  } finally {
    keyActionLoading.value = false
  }
}

async function handleDeleteKey(): Promise<void> {
  if (!window.confirm(t('admin.usageHub.confirm.deleteKey'))) {
    return
  }
  keyActionLoading.value = true
  try {
    await hubStepUp.run(() => adminAPI.usageHub.deleteKey())
    newlyGeneratedKey.value = ''
    config.value = {
      ...config.value,
      enabled: false,
      has_key: false,
      masked_key: ''
    }
    appStore.showSuccess(t('admin.usageHub.messages.keyDeleted'))
  } catch (error) {
    if (isStepUpCancelled(error) || reportStepUpBlocked(error)) return
    appStore.showError(extractErrorMessage(error))
  } finally {
    keyActionLoading.value = false
  }
}

async function handleRefreshAccount(item: UsageHubAccountItem): Promise<void> {
  const nextSet = new Set(refreshingAccountIds.value)
  nextSet.add(item.id)
  refreshingAccountIds.value = nextSet
  try {
    const updated = await adminAPI.usageHub.refreshAccount(item.id)
    const idx = overview.value.accounts.findIndex((a) => a.id === item.id)
    if (idx >= 0) {
      overview.value.accounts.splice(idx, 1, updated)
    }
    overview.value = await adminAPI.usageHub.listAccounts()
    appStore.showSuccess(t('admin.usageHub.messages.accountRefreshed', { name: item.name }))
  } catch (error) {
    appStore.showError(extractErrorMessage(error))
  } finally {
    const cleanup = new Set(refreshingAccountIds.value)
    cleanup.delete(item.id)
    refreshingAccountIds.value = cleanup
  }
}

async function handleRefreshAll(): Promise<void> {
  refreshingAll.value = true
  try {
    const res = await adminAPI.usageHub.refreshAllAccounts()
    if (res.overview) {
      overview.value = res.overview
    }
    appStore.showSuccess(
      t('admin.usageHub.messages.batchRefreshed', {
        refreshed: res.refreshed,
        failed: res.failed
      })
    )
  } catch (error) {
    appStore.showError(extractErrorMessage(error))
  } finally {
    refreshingAll.value = false
  }
}

async function openPreviewModal(): Promise<void> {
  previewLoading.value = true
  try {
    const preview = await adminAPI.usageHub.previewAuthFiles()
    previewFileCount.value = Array.isArray(preview.files) ? preview.files.length : 0
    previewJSON.value = JSON.stringify(preview, null, 2)
    previewModalOpen.value = true
  } catch (error) {
    appStore.showError(extractErrorMessage(error))
  } finally {
    previewLoading.value = false
  }
}

onMounted(() => {
  void loadAll()
})
</script>
