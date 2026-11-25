<template>
  <AppLayout>
    <div class="space-y-6">
      <!-- 管理员关闭自助模式提示 -->
      <div
        v-if="!loading && !config.self_service_enabled && !config.is_admin"
        class="rounded-xl border border-amber-300 bg-amber-50 p-5 dark:border-amber-700/60 dark:bg-amber-950/30"
        data-testid="self-service-disabled-banner"
      >
        <h2 class="text-base font-semibold text-amber-900 dark:text-amber-200">
          {{ t('usageHub.selfServiceDisabledTitle') }}
        </h2>
        <p class="mt-1 text-sm text-amber-800/90 dark:text-amber-300/90">
          {{ t('usageHub.selfServiceDisabledDesc') }}
        </p>
      </div>

      <!-- 卡片 1：个人 CPA Hub 接入与独立只读密钥 -->
      <div class="card p-6">
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div class="space-y-1">
            <div class="flex flex-wrap items-center gap-2.5">
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('usageHub.configTitle') }}
              </h2>
              <span
                class="inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium"
                :class="
                  config.enabled
                    ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
                    : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
                "
              >
                {{ config.enabled ? t('usageHub.statusEnabled') : t('usageHub.statusDisabled') }}
              </span>
            </div>
            <p class="text-sm text-gray-500 dark:text-gray-400">
              {{ t('usageHub.configSubtitle') }}
            </p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="previewLoading || isSelfServiceBlocked"
              data-testid="user-preview-refresh-btn"
              @click="loadPreview"
            >
              {{ previewLoading ? t('common.loading') : t('usageHub.preview.refreshButton') }}
            </button>
          </div>
        </div>

        <!-- 个人配置开关区 -->
        <div class="mt-5 flex flex-wrap items-center gap-6 border-t border-gray-100 pt-4 dark:border-dark-700">
          <label class="inline-flex cursor-pointer items-center gap-2.5 text-sm font-medium text-gray-800 dark:text-gray-200">
            <input
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="config.enabled"
              :disabled="savingConfig || isSelfServiceBlocked"
              data-testid="user-hub-enabled-toggle"
              @change="handleToggleConfig({ enabled: ($event.target as HTMLInputElement).checked })"
            />
            <span>{{ t('usageHub.enableLabel') }}</span>
          </label>

          <label class="inline-flex cursor-pointer items-center gap-2.5 text-sm text-gray-700 dark:text-gray-300">
            <input
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="config.include_sub2api"
              :disabled="savingConfig || isSelfServiceBlocked"
              data-testid="user-hub-include-sub2api-toggle"
              @change="handleToggleConfig({ include_sub2api: ($event.target as HTMLInputElement).checked })"
            />
            <span>{{ t('usageHub.includeSub2APILabel') }}</span>
          </label>

          <label
            v-if="config.is_admin"
            class="inline-flex cursor-pointer items-center gap-2.5 text-sm text-gray-700 dark:text-gray-300"
          >
            <input
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="config.include_disabled"
              :disabled="savingConfig"
              data-testid="user-hub-include-disabled-toggle"
              @change="handleToggleConfig({ include_disabled: ($event.target as HTMLInputElement).checked })"
            />
            <span>{{ t('usageHub.includeDisabledLabel') }}</span>
          </label>

          <label
            v-if="config.is_admin"
            class="inline-flex cursor-pointer items-center gap-2.5 text-sm text-gray-700 dark:text-gray-300"
          >
            <input
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="config.expose_reset_credits"
              :disabled="savingConfig"
              data-testid="user-hub-expose-reset-toggle"
              @change="handleToggleConfig({ expose_reset_credits: ($event.target as HTMLInputElement).checked })"
            />
            <span>{{ t('usageHub.exposeResetCreditsLabel') }}</span>
          </label>
        </div>

        <!-- Base URL 与个人密钥 -->
        <div class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2">
          <div class="rounded-xl border border-gray-200 bg-gray-50/70 p-4 dark:border-dark-700 dark:bg-dark-900/50">
            <div class="mb-1.5 text-xs font-medium text-gray-600 dark:text-gray-400">
              {{ t('usageHub.hubUrlLabel') }}
            </div>
            <div class="flex items-center gap-2">
              <input
                :value="hubBaseUrl"
                readonly
                class="input flex-1 font-mono text-xs"
                data-testid="user-hub-base-url-input"
              />
              <button
                type="button"
                class="btn btn-secondary btn-sm shrink-0"
                @click="copyText(hubBaseUrl)"
              >
                {{ t('usageHub.actions.copy') }}
              </button>
            </div>
            <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
              {{ t('usageHub.hubUrlHint') }}
            </p>
          </div>

          <div class="rounded-xl border border-gray-200 bg-gray-50/70 p-4 dark:border-dark-700 dark:bg-dark-900/50">
            <div class="mb-1.5 text-xs font-medium text-gray-600 dark:text-gray-400">
              {{ t('usageHub.personalKeyLabel') }}
            </div>
            <div class="flex flex-wrap items-center gap-2">
              <div
                class="flex-1 truncate rounded-lg border border-gray-200 bg-white px-3 py-2 font-mono text-xs text-gray-700 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-200"
                data-testid="user-hub-masked-key"
              >
                {{ config.has_key ? config.masked_key : t('usageHub.personalKeyNotConfigured') }}
              </div>
              <button
                type="button"
                class="btn btn-secondary btn-sm shrink-0"
                :disabled="keyActionLoading || isSelfServiceBlocked"
                data-testid="user-regenerate-key-btn"
                @click="handleRegenerateKey"
              >
                {{
                  config.has_key
                    ? t('usageHub.actions.regenerateKey')
                    : t('usageHub.actions.generateKey')
                }}
              </button>
              <button
                v-if="config.has_key"
                type="button"
                class="btn btn-danger btn-sm shrink-0"
                :disabled="keyActionLoading"
                data-testid="user-delete-key-btn"
                @click="handleDeleteKey"
              >
                {{ t('usageHub.actions.deleteKey') }}
              </button>
            </div>
            <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
              {{ t('usageHub.securityHint', { seconds: config.cache_ttl_seconds || 30 }) }}
            </p>
          </div>
        </div>

        <!-- 新生成个人明文 Key 一次性展示横幅 -->
        <div
          v-if="newlyGeneratedKey"
          class="mt-4 rounded-xl border border-amber-300 bg-amber-50 p-4 dark:border-amber-700/60 dark:bg-amber-950/30"
          data-testid="user-generated-key-banner"
        >
          <div class="flex flex-wrap items-start justify-between gap-2">
            <div>
              <h3 class="text-sm font-semibold text-amber-900 dark:text-amber-200">
                {{ t('usageHub.newKeyBannerTitle') }}
              </h3>
              <p class="mt-0.5 text-xs text-amber-800/90 dark:text-amber-300/90">
                {{ t('usageHub.newKeyBannerDesc') }}
              </p>
            </div>
            <button
              type="button"
              class="text-xs font-medium text-amber-700 hover:underline dark:text-amber-300"
              @click="newlyGeneratedKey = ''"
            >
              {{ t('usageHub.actions.close') }}
            </button>
          </div>
          <div class="mt-3 flex items-center gap-2">
            <input
              :value="newlyGeneratedKey"
              readonly
              class="input flex-1 font-mono text-xs"
              data-testid="user-generated-key-value"
            />
            <button
              type="button"
              class="btn btn-primary btn-sm shrink-0"
              @click="copyText(newlyGeneratedKey)"
            >
              {{ t('usageHub.actions.copy') }}
            </button>
          </div>
          <div class="mt-3">
            <div class="mb-1 text-xs font-medium text-amber-900/90 dark:text-amber-200/90">
              {{ t('usageHub.curlExampleLabel') }}
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
                {{ t('usageHub.actions.copy') }}
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- 卡片 2：自定义 Magpie 数据源（整形成 CPA Hub 数据） -->
      <div class="card p-6">
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div class="space-y-1">
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">
              {{ t('usageHub.magpie.title') }}
            </h3>
            <p class="text-sm text-gray-500 dark:text-gray-400">
              {{ t('usageHub.magpie.subtitle') }}
            </p>
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{
                config.allow_private_magpie_url
                  ? t('usageHub.magpie.adminPrivateHint')
                  : t('usageHub.magpie.httpsOnlyHint')
              }}
            </p>
          </div>
          <button
            v-if="config.allow_magpie"
            type="button"
            class="btn btn-primary btn-sm shrink-0"
            :disabled="isSelfServiceBlocked || config.magpie_sources.length >= config.max_magpie_sources"
            data-testid="add-magpie-source-btn"
            @click="openAddMagpieModal"
          >
            {{ t('usageHub.magpie.addButton') }}
          </button>
        </div>

        <div
          v-if="!config.allow_magpie"
          class="mt-4 rounded-lg border border-gray-200 bg-gray-50 p-4 text-sm text-gray-500 dark:border-dark-700 dark:bg-dark-900/50 dark:text-gray-400"
        >
          {{ t('usageHub.magpie.disabledHint') }}
        </div>

        <div
          v-else-if="config.magpie_sources.length === 0"
          class="mt-4 rounded-xl border border-dashed border-gray-200 p-6 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400"
        >
          <p>{{ t('usageHub.magpie.empty') }}</p>
          <p class="mt-2 font-mono text-xs text-gray-400 dark:text-gray-500">
            {{ t('usageHub.magpie.recommendedReadOnlyKeyHint') }}
          </p>
        </div>

        <div v-else class="mt-4 space-y-3">
          <div
            v-for="source in config.magpie_sources"
            :key="source.id"
            class="flex flex-wrap items-center justify-between gap-4 rounded-xl border border-gray-200 bg-gray-50/60 p-4 dark:border-dark-700 dark:bg-dark-900/40"
            :data-testid="`magpie-source-${source.id}`"
          >
            <div class="min-w-0 flex-1 space-y-1">
              <div class="flex flex-wrap items-center gap-2">
                <span class="font-medium text-gray-900 dark:text-white">{{ source.name }}</span>
                <span
                  class="inline-flex rounded-full px-2 py-0.5 text-[11px] font-medium"
                  :class="
                    source.enabled
                      ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
                      : 'bg-gray-200 text-gray-600 dark:bg-dark-700 dark:text-gray-400'
                  "
                >
                  {{ source.enabled ? t('usageHub.statusEnabled') : t('usageHub.statusDisabled') }}
                </span>
                <span
                  class="inline-flex rounded px-2 py-0.5 text-[11px] font-medium"
                  :class="
                    source.has_api_key
                      ? 'bg-blue-50 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
                      : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-gray-400'
                  "
                >
                  {{
                    source.has_api_key
                      ? `${t('usageHub.magpie.keySavedBadge')} (${source.masked_api_key})`
                      : t('usageHub.magpie.noKeyBadge')
                  }}
                </span>
              </div>
              <div class="truncate font-mono text-xs text-gray-500 dark:text-gray-400">
                {{ source.base_url }}
              </div>
              <div class="flex flex-wrap items-center gap-1.5 pt-0.5">
                <span
                  v-for="platform in source.providers"
                  :key="platform"
                  class="inline-flex rounded bg-indigo-100 px-2 py-0.5 text-[11px] font-medium text-indigo-700 dark:bg-indigo-900/40 dark:text-indigo-300"
                >
                  {{ platformLabel(platform) }}
                </span>
              </div>
            </div>

            <div class="flex flex-wrap items-center gap-2">
              <button
                type="button"
                class="btn btn-secondary btn-xs"
                :disabled="testingSourceId === source.id"
                :data-testid="`test-magpie-source-${source.id}`"
                @click="handleTestSavedSource(source)"
              >
                {{
                  testingSourceId === source.id
                    ? t('usageHub.actions.testing')
                    : t('usageHub.actions.testConnection')
                }}
              </button>
              <button
                type="button"
                class="btn btn-secondary btn-xs"
                :disabled="savingConfig"
                :data-testid="`edit-magpie-source-${source.id}`"
                @click="openEditMagpieModal(source)"
              >
                {{ t('usageHub.actions.edit') }}
              </button>
              <button
                type="button"
                class="btn btn-danger btn-xs"
                :disabled="savingConfig"
                :data-testid="`delete-magpie-source-${source.id}`"
                @click="handleDeleteMagpieSource(source)"
              >
                {{ t('usageHub.actions.delete') }}
              </button>
            </div>
          </div>
          <p class="text-xs text-gray-400 dark:text-gray-500">
            {{ t('usageHub.magpie.limitHint', { max: config.max_magpie_sources }) }} ·
            {{ t('usageHub.magpie.recommendedReadOnlyKeyHint') }}
          </p>
        </div>
      </div>

      <!-- 卡片 3：当前个人 Hub 合并账号预览 -->
      <div class="card overflow-hidden">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 p-5 dark:border-dark-700">
          <div>
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">
              {{ t('usageHub.preview.title') }}
            </h3>
            <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t('usageHub.preview.subtitle') }}
            </p>
          </div>
          <div class="flex items-center gap-2">
            <span class="rounded-full bg-gray-100 px-2.5 py-0.5 text-xs font-medium text-gray-700 dark:bg-dark-700 dark:text-gray-200">
              {{ t('usageHub.preview.fileCount', { count: previewFiles.length }) }}
            </span>
            <button
              type="button"
              class="btn btn-secondary btn-xs"
              data-testid="open-raw-json-btn"
              @click="rawJsonModalOpen = true"
            >
              {{ t('usageHub.preview.viewJsonButton') }}
            </button>
          </div>
        </div>

        <!-- 非致命 Magpie 拉取警告 -->
        <div
          v-if="previewWarnings.length > 0"
          class="border-b border-amber-200 bg-amber-50 px-5 py-3 text-xs text-amber-800 dark:border-amber-800/50 dark:bg-amber-950/30 dark:text-amber-200"
          data-testid="preview-warnings-box"
        >
          <div class="font-semibold">{{ t('usageHub.preview.warningsTitle') }}</div>
          <ul class="mt-1 list-disc space-y-0.5 pl-4">
            <li v-for="w in previewWarnings" :key="w.source_id">
              <span class="font-medium">{{ w.source_name }}</span>: {{ w.message }}
            </li>
          </ul>
        </div>

        <div v-if="loading || previewLoading" class="p-8 text-center text-sm text-gray-500 dark:text-gray-400">
          {{ t('common.loading') }}
        </div>

        <div
          v-else-if="previewFiles.length === 0"
          class="p-8 text-center text-sm text-gray-500 dark:text-gray-400"
        >
          {{ t('usageHub.preview.empty') }}
        </div>

        <div v-else class="divide-y divide-gray-100 dark:divide-dark-800">
          <div
            v-for="file in previewFiles"
            :key="file.auth_index"
            class="flex flex-wrap items-center justify-between gap-3 px-5 py-3.5 hover:bg-gray-50/70 dark:hover:bg-dark-700/30"
            :data-testid="`preview-file-${file.auth_index}`"
          >
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
                <span
                  class="inline-flex rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase"
                  :class="
                    file.source === 'magpie'
                      ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/40 dark:text-purple-300'
                      : 'bg-sky-100 text-sky-700 dark:bg-sky-900/40 dark:text-sky-300'
                  "
                >
                  {{ file.source === 'magpie' ? t('usageHub.preview.sourceMagpie') : t('usageHub.preview.sourceSub2API') }}
                </span>
                <span class="inline-flex rounded bg-indigo-100 px-1.5 py-0.5 text-[10px] font-medium uppercase text-indigo-700 dark:bg-indigo-900/40 dark:text-indigo-300">
                  {{ file.provider }}
                </span>
                <span class="font-medium text-gray-900 dark:text-white">{{ file.label || file.name }}</span>
                <span
                  v-if="file.plan_type"
                  class="rounded bg-gray-100 px-1.5 py-0.5 text-[10px] uppercase text-gray-600 dark:bg-dark-700 dark:text-gray-300"
                >
                  {{ file.plan_type }}
                </span>
              </div>
              <div class="mt-0.5 flex flex-wrap items-center gap-2 font-mono text-[11px] text-gray-400 dark:text-gray-500">
                <span v-if="file.email">{{ file.email }}</span>
                <span>{{ file.auth_index }}</span>
              </div>
              <div v-if="file.status_message" class="mt-0.5 text-xs text-rose-600 dark:text-rose-400">
                {{ file.status_message }}
              </div>
            </div>
            <span
              class="inline-flex rounded-full px-2.5 py-0.5 text-xs font-medium"
              :class="
                file.status === 'error'
                  ? 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300'
                  : file.unavailable
                    ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
                    : 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
              "
            >
              {{ file.status }}
            </span>
          </div>
        </div>
      </div>
    </div>

    <!-- 添加 / 编辑 Magpie 数据源弹窗 -->
    <div
      v-if="magpieModalOpen"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      data-testid="magpie-source-modal"
      @click.self="closeMagpieModal"
    >
      <div class="w-full max-w-lg overflow-hidden rounded-2xl bg-white shadow-2xl dark:bg-dark-800">
        <div class="flex items-center justify-between border-b border-gray-200 px-6 py-4 dark:border-dark-700">
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">
            {{ editingSourceId ? t('usageHub.actions.edit') : t('usageHub.magpie.addButton') }}
          </h3>
          <button type="button" class="btn btn-secondary btn-xs" @click="closeMagpieModal">
            {{ t('usageHub.actions.close') }}
          </button>
        </div>

        <div class="space-y-4 p-6">
          <div>
            <label class="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300">
              {{ t('usageHub.magpie.fields.name') }}
            </label>
            <input
              v-model.trim="magpieForm.name"
              type="text"
              class="input w-full text-sm"
              :placeholder="t('usageHub.magpie.fields.namePlaceholder')"
              data-testid="magpie-form-name"
            />
          </div>

          <div>
            <label class="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300">
              {{ t('usageHub.magpie.fields.baseUrl') }}
            </label>
            <input
              v-model.trim="magpieForm.base_url"
              type="url"
              class="input w-full font-mono text-sm"
              :placeholder="t('usageHub.magpie.fields.baseUrlPlaceholder')"
              data-testid="magpie-form-url"
            />
          </div>

          <div>
            <label class="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300">
              {{ t('usageHub.magpie.fields.apiKey') }}
            </label>
            <input
              v-model.trim="magpieForm.api_key"
              type="password"
              autocomplete="off"
              class="input w-full font-mono text-sm"
              :placeholder="
                editingHasSavedKey
                  ? t('usageHub.magpie.fields.apiKeyKeepPlaceholder')
                  : t('usageHub.magpie.fields.apiKeyPlaceholder')
              "
              data-testid="magpie-form-key"
            />
          </div>

          <div>
            <div class="mb-1.5 text-xs font-medium text-gray-700 dark:text-gray-300">
              {{ t('usageHub.magpie.fields.platforms') }}
            </div>
            <div class="flex flex-wrap gap-4">
              <label
                v-for="platform in availableMagpiePlatforms"
                :key="platform"
                class="inline-flex cursor-pointer items-center gap-2 text-sm text-gray-700 dark:text-gray-300"
              >
                <input
                  type="checkbox"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                  :checked="magpieForm.providers.includes(platform)"
                  :data-testid="`magpie-form-platform-${platform}`"
                  @change="toggleFormPlatform(platform, ($event.target as HTMLInputElement).checked)"
                />
                <span>{{ platformLabel(platform) }}</span>
              </label>
            </div>
          </div>

          <label class="inline-flex cursor-pointer items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input
              v-model="magpieForm.enabled"
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              data-testid="magpie-form-enabled"
            />
            <span>{{ t('usageHub.magpie.fields.enabled') }}</span>
          </label>

          <!-- 连通性测试结果 -->
          <div
            v-if="modalTestResult"
            class="rounded-xl border p-3 text-xs"
            :class="
              modalTestResult.ok
                ? 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-800/50 dark:bg-emerald-950/30 dark:text-emerald-200'
                : 'border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-800/50 dark:bg-rose-950/30 dark:text-rose-200'
            "
            data-testid="magpie-test-result"
          >
            <div class="font-medium">
              {{
                modalTestResult.ok
                  ? t('usageHub.magpie.testResultSuccess', {
                      total: modalTestResult.total_raw_quotas,
                      matched: modalTestResult.matched_count,
                      breakdown: formatProviderBreakdown(modalTestResult.provider_counts)
                    })
                  : t('usageHub.magpie.testResultFailed', { error: modalTestResult.error || '' })
              }}
            </div>
            <div v-if="modalTestResult.ok && modalTestResult.sample_labels.length > 0" class="mt-1.5 flex flex-wrap gap-1.5">
              <span
                v-for="sample in modalTestResult.sample_labels"
                :key="sample"
                class="rounded bg-white/80 px-1.5 py-0.5 font-mono text-[11px] text-emerald-800 dark:bg-dark-800 dark:text-emerald-300"
              >
                {{ sample }}
              </span>
            </div>
          </div>
        </div>

        <div class="flex items-center justify-between gap-3 border-t border-gray-200 px-6 py-4 dark:border-dark-700">
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="modalTesting || !magpieForm.base_url"
            data-testid="magpie-modal-test-btn"
            @click="handleTestModalForm"
          >
            {{ modalTesting ? t('usageHub.actions.testing') : t('usageHub.actions.testConnection') }}
          </button>
          <div class="flex items-center gap-2">
            <button type="button" class="btn btn-secondary btn-sm" @click="closeMagpieModal">
              {{ t('usageHub.actions.cancel') }}
            </button>
            <button
              type="button"
              class="btn btn-primary btn-sm"
              :disabled="savingConfig || !magpieForm.base_url"
              data-testid="magpie-modal-save-btn"
              @click="handleSaveMagpieModal"
            >
              {{ savingConfig ? t('usageHub.actions.saving') : t('usageHub.actions.save') }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- 原始 auth-files JSON 预览弹窗 -->
    <div
      v-if="rawJsonModalOpen"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      data-testid="user-raw-json-modal"
      @click.self="rawJsonModalOpen = false"
    >
      <div class="flex max-h-[85vh] w-full max-w-3xl flex-col overflow-hidden rounded-2xl bg-white shadow-2xl dark:bg-dark-800">
        <div class="flex items-start justify-between gap-4 border-b border-gray-200 px-6 py-4 dark:border-dark-700">
          <div>
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">
              {{ t('usageHub.preview.modalTitle') }}
            </h3>
            <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t('usageHub.preview.modalSubtitle') }}
            </p>
          </div>
          <button type="button" class="btn btn-secondary btn-xs" @click="rawJsonModalOpen = false">
            {{ t('usageHub.actions.close') }}
          </button>
        </div>
        <div class="flex items-center justify-between bg-gray-50 px-6 py-2 text-xs text-gray-600 dark:bg-dark-900/60 dark:text-gray-300">
          <span>{{ t('usageHub.preview.fileCount', { count: previewFiles.length }) }}</span>
          <button
            type="button"
            class="text-primary-600 hover:underline dark:text-primary-400"
            @click="copyText(rawPreviewJson)"
          >
            {{ t('usageHub.actions.copy') }}
          </button>
        </div>
        <pre class="flex-1 overflow-auto bg-gray-950 p-5 font-mono text-xs leading-relaxed text-emerald-300">{{ rawPreviewJson }}</pre>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { usageHubAPI } from '@/api'
import type {
  TestMagpieSourceResult,
  UpdateUserUsageHubConfigPayload,
  UsageHubMagpiePlatform,
  UsageHubMagpieSourceInput,
  UsageHubMagpieSourceView,
  UsageHubPreviewFile,
  UsageHubPreviewWarning,
  UserUsageHubConfig
} from '@/api/usageHub'
import { useAppStore } from '@/stores'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const savingConfig = ref(false)
const keyActionLoading = ref(false)
const previewLoading = ref(false)
const testingSourceId = ref('')
const modalTesting = ref(false)

const config = ref<UserUsageHubConfig>({
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

const newlyGeneratedKey = ref('')
const previewFiles = ref<UsageHubPreviewFile[]>([])
const previewWarnings = ref<UsageHubPreviewWarning[]>([])
const rawJsonModalOpen = ref(false)

const magpieModalOpen = ref(false)
const editingSourceId = ref('')
const editingHasSavedKey = ref(false)
const modalTestResult = ref<TestMagpieSourceResult | null>(null)
const magpieForm = ref<{
  name: string
  base_url: string
  api_key: string
  enabled: boolean
  providers: UsageHubMagpiePlatform[]
}>({
  name: '',
  base_url: '',
  api_key: '',
  enabled: true,
  providers: ['codex', 'claude', 'antigravity']
})

const availableMagpiePlatforms = computed<UsageHubMagpiePlatform[]>(() =>
  config.value.magpie_platforms.length > 0
    ? config.value.magpie_platforms
    : ['codex', 'claude', 'antigravity']
)

const isSelfServiceBlocked = computed(
  () => !config.value.self_service_enabled && !config.value.is_admin
)

const hubBaseUrl = computed(() => {
  if (typeof window !== 'undefined' && window.location?.origin) {
    return window.location.origin
  }
  return ''
})

const curlExampleCommand = computed(() => {
  const base = hubBaseUrl.value || 'https://your-sub2api-host'
  const key = newlyGeneratedKey.value || '<YOUR_PERSONAL_HUB_KEY>'
  return `curl -s "${base}${config.value.auth_files_path}" -H "Authorization: Bearer ${key}"`
})

const rawPreviewJson = computed(() =>
  JSON.stringify({ files: previewFiles.value }, null, 2)
)

function platformLabel(platform: UsageHubMagpiePlatform): string {
  return t(`usageHub.magpie.platforms.${platform}`)
}

function formatProviderBreakdown(counts: Record<string, number>): string {
  const entries = Object.entries(counts || {}).filter(([, count]) => count > 0)
  if (entries.length === 0) {
    return t('usageHub.magpie.testResultEmptyBreakdown')
  }
  return entries.map(([provider, count]) => `${provider}: ${count}`).join(', ')
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
    appStore.showSuccess(t('usageHub.actions.copied'))
  } catch {
    appStore.showError(t('usageHub.actions.copyFailed'))
  }
}

async function loadPreview(): Promise<void> {
  if (isSelfServiceBlocked.value) {
    previewFiles.value = []
    previewWarnings.value = []
    return
  }
  previewLoading.value = true
  try {
    const res = await usageHubAPI.previewAuthFiles()
    previewFiles.value = Array.isArray(res.files) ? res.files : []
    previewWarnings.value = Array.isArray(res.warnings) ? res.warnings : []
  } catch (error) {
    appStore.showError(extractErrorMessage(error))
  } finally {
    previewLoading.value = false
  }
}

async function loadAll(): Promise<void> {
  loading.value = true
  try {
    const cfg = await usageHubAPI.getConfig()
    config.value = cfg
    if (cfg.self_service_enabled || cfg.is_admin) {
      await loadPreview()
    }
  } catch (error) {
    appStore.showError(extractErrorMessage(error))
  } finally {
    loading.value = false
  }
}

async function handleToggleConfig(payload: UpdateUserUsageHubConfigPayload): Promise<void> {
  savingConfig.value = true
  try {
    const updated = await usageHubAPI.updateConfig(payload)
    config.value = updated
    if (updated.generated_key) {
      newlyGeneratedKey.value = updated.generated_key
    }
    appStore.showSuccess(t('usageHub.messages.configSaved'))
    await loadPreview()
  } catch (error) {
    appStore.showError(extractErrorMessage(error))
  } finally {
    savingConfig.value = false
  }
}

async function handleRegenerateKey(): Promise<void> {
  if (config.value.has_key && !window.confirm(t('usageHub.confirm.regenerateKey'))) {
    return
  }
  keyActionLoading.value = true
  try {
    const res = await usageHubAPI.regenerateKey()
    newlyGeneratedKey.value = res.key
    config.value = {
      ...config.value,
      has_key: true,
      masked_key: res.masked_key
    }
    appStore.showSuccess(t('usageHub.messages.keyGenerated'))
  } catch (error) {
    appStore.showError(extractErrorMessage(error))
  } finally {
    keyActionLoading.value = false
  }
}

async function handleDeleteKey(): Promise<void> {
  if (!window.confirm(t('usageHub.confirm.deleteKey'))) {
    return
  }
  keyActionLoading.value = true
  try {
    await usageHubAPI.deleteKey()
    newlyGeneratedKey.value = ''
    config.value = {
      ...config.value,
      enabled: false,
      has_key: false,
      masked_key: ''
    }
    appStore.showSuccess(t('usageHub.messages.keyDeleted'))
  } catch (error) {
    appStore.showError(extractErrorMessage(error))
  } finally {
    keyActionLoading.value = false
  }
}

function openAddMagpieModal(): void {
  editingSourceId.value = ''
  editingHasSavedKey.value = false
  modalTestResult.value = null
  magpieForm.value = {
    name: '',
    base_url: '',
    api_key: '',
    enabled: true,
    providers: [...availableMagpiePlatforms.value]
  }
  magpieModalOpen.value = true
}

function openEditMagpieModal(source: UsageHubMagpieSourceView): void {
  editingSourceId.value = source.id
  editingHasSavedKey.value = source.has_api_key
  modalTestResult.value = null
  magpieForm.value = {
    name: source.name,
    base_url: source.base_url,
    api_key: '',
    enabled: source.enabled,
    providers: source.providers.length > 0 ? [...source.providers] : [...availableMagpiePlatforms.value]
  }
  magpieModalOpen.value = true
}

function closeMagpieModal(): void {
  magpieModalOpen.value = false
  modalTestResult.value = null
}

function toggleFormPlatform(platform: UsageHubMagpiePlatform, checked: boolean): void {
  const next = new Set(magpieForm.value.providers)
  if (checked) {
    next.add(platform)
  } else {
    next.delete(platform)
  }
  magpieForm.value.providers = availableMagpiePlatforms.value.filter((p) => next.has(p))
}

async function handleTestModalForm(): Promise<void> {
  if (!magpieForm.value.base_url) {
    appStore.showError(t('usageHub.messages.baseUrlRequired'))
    return
  }
  modalTesting.value = true
  modalTestResult.value = null
  try {
    modalTestResult.value = await usageHubAPI.testMagpieSource({
      source_id: editingSourceId.value || undefined,
      base_url: magpieForm.value.base_url,
      api_key: magpieForm.value.api_key || undefined,
      providers: magpieForm.value.providers
    })
  } catch (error) {
    modalTestResult.value = {
      ok: false,
      total_raw_quotas: 0,
      matched_count: 0,
      provider_counts: {},
      sample_labels: [],
      error: extractErrorMessage(error)
    }
  } finally {
    modalTesting.value = false
  }
}

async function handleTestSavedSource(source: UsageHubMagpieSourceView): Promise<void> {
  testingSourceId.value = source.id
  try {
    const res = await usageHubAPI.testMagpieSource({
      source_id: source.id,
      base_url: source.base_url,
      providers: source.providers
    })
    if (res.ok) {
      appStore.showSuccess(
        t('usageHub.magpie.testResultSuccess', {
          total: res.total_raw_quotas,
          matched: res.matched_count,
          breakdown: formatProviderBreakdown(res.provider_counts)
        })
      )
    } else {
      appStore.showError(t('usageHub.magpie.testResultFailed', { error: res.error || '' }))
    }
  } catch (error) {
    appStore.showError(extractErrorMessage(error))
  } finally {
    testingSourceId.value = ''
  }
}

function toMagpieSourceInputs(sources: UsageHubMagpieSourceView[]): UsageHubMagpieSourceInput[] {
  return sources.map((s) => ({
    id: s.id,
    name: s.name,
    enabled: s.enabled,
    base_url: s.base_url,
    providers: s.providers
  }))
}

async function handleSaveMagpieModal(): Promise<void> {
  if (!magpieForm.value.base_url) {
    appStore.showError(t('usageHub.messages.baseUrlRequired'))
    return
  }
  const providers =
    magpieForm.value.providers.length > 0
      ? magpieForm.value.providers
      : [...availableMagpiePlatforms.value]

  const nextSources = toMagpieSourceInputs(config.value.magpie_sources)
  const payloadItem: UsageHubMagpieSourceInput = {
    id: editingSourceId.value || undefined,
    name: magpieForm.value.name,
    enabled: magpieForm.value.enabled,
    base_url: magpieForm.value.base_url,
    api_key: magpieForm.value.api_key || undefined,
    providers
  }

  if (editingSourceId.value) {
    const idx = nextSources.findIndex((item) => item.id === editingSourceId.value)
    if (idx >= 0) {
      nextSources.splice(idx, 1, payloadItem)
    } else {
      nextSources.push(payloadItem)
    }
  } else {
    nextSources.push(payloadItem)
  }

  savingConfig.value = true
  try {
    const updated = await usageHubAPI.updateConfig({ magpie_sources: nextSources })
    config.value = updated
    if (updated.generated_key) {
      newlyGeneratedKey.value = updated.generated_key
    }
    closeMagpieModal()
    appStore.showSuccess(t('usageHub.messages.magpieSaved'))
    await loadPreview()
  } catch (error) {
    appStore.showError(extractErrorMessage(error))
  } finally {
    savingConfig.value = false
  }
}

async function handleDeleteMagpieSource(source: UsageHubMagpieSourceView): Promise<void> {
  if (!window.confirm(t('usageHub.confirm.deleteMagpieSource', { name: source.name }))) {
    return
  }
  const nextSources = toMagpieSourceInputs(
    config.value.magpie_sources.filter((item) => item.id !== source.id)
  )
  savingConfig.value = true
  try {
    const updated = await usageHubAPI.updateConfig({ magpie_sources: nextSources })
    config.value = updated
    appStore.showSuccess(t('usageHub.messages.magpieDeleted'))
    await loadPreview()
  } catch (error) {
    appStore.showError(extractErrorMessage(error))
  } finally {
    savingConfig.value = false
  }
}

onMounted(() => {
  void loadAll()
})
</script>
