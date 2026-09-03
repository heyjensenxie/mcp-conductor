<template>
  <div>
    <a-card :bordered="true" :title="t('settings.apiCredential')" class="mb">
      <a-row :gutter="16">
        <a-col :span="10">
          <a-input v-model:value="token" type="password" :placeholder="t('settings.apiKeyPlaceholder')" />
        </a-col>
        <a-col>
          <a-button type="primary" @click="save">{{ t('settings.save') }}</a-button>
        </a-col>
      </a-row>
    </a-card>

    <a-card :bordered="true" :title="t('settings.about')" class="mb">
      <a-descriptions :column="1" bordered size="small">
        <a-descriptions-item :label="t('settings.version')">0.1.0</a-descriptions-item>
        <a-descriptions-item :label="t('settings.mcpEndpoint')">
          <code>/mcp</code>（streamable HTTP · tools/list · tools/call）
        </a-descriptions-item>
        <a-descriptions-item :label="t('settings.controlApi')">
          <code>/api/*</code>（code / message / request_id）
        </a-descriptions-item>
        <a-descriptions-item :label="t('settings.configDesc')">
          config.yaml + CONDUCTOR_*（server / database / redis / gateway / ratelimit / auth / credentials / logging / observability）
        </a-descriptions-item>
      </a-descriptions>
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
const store = useAppStore()
const token = ref(store.token)

function save() {
  store.setToken(token.value.trim())
  message.success(t('settings.savedOk'))
}
</script>

<style scoped>
.mb {
  margin-bottom: 16px;
}
code {
  background: #f5f5f5;
  padding: 2px 6px;
  border-radius: 4px;
}
</style>