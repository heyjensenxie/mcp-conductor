<template>
  <div>
    <a-card :bordered="true" :title="t('settings.sessionCard')" class="mb">
      <a-alert v-if="authRequired === false" type="info" show-icon :message="t('settings.noAuthInfo')" class="mb" />
      <div v-else-if="store.token" class="session-row">
        <a-tag color="green">{{ t('settings.signedIn') }}</a-tag>
        <a-tag class="mono">{{ userTag }}</a-tag>
        <a-popconfirm :title="t('auth.confirmLogout')" :ok-text="t('auth.logout')" :cancel-text="t('common.cancel')" @confirm="doLogout">
          <a-button danger size="small">{{ t('auth.logout') }}</a-button>
        </a-popconfirm>
      </div>
      <div v-else class="session-row">
        <a-tag>{{ t('settings.notSignedIn') }}</a-tag>
        <a-button type="primary" size="small" @click="router.push('/login')">{{ t('settings.goLogin') }}</a-button>
      </div>
    </a-card>

    <a-card :bordered="true" class="mb">
      <a-collapse :bordered="false">
        <a-collapse-panel :key="'token'" :header="t('settings.advancedTitle')">
          <a-row :gutter="16">
            <a-col :span="14">
              <a-input v-model:value="token" type="password" :placeholder="t('settings.apiKeyPlaceholder')" />
            </a-col>
            <a-col>
              <a-button type="primary" @click="save">{{ t('settings.save') }}</a-button>
            </a-col>
          </a-row>
        </a-collapse-panel>
      </a-collapse>
    </a-card>

    <a-card :bordered="true" :title="t('settings.about')" class="mb">
      <a-descriptions :column="1" bordered size="small">
        <a-descriptions-item :label="t('settings.version')">0.1.0</a-descriptions-item>
        <a-descriptions-item :label="t('settings.mcpEndpoint')">
          <code>/mcp</code>（streamable HTTP · tools/list · tools/call）
        </a-descriptions-item>
        <a-descriptions-item :label="t('settings.controlApi')">
          <code>/api/*</code>（code / message / request_id；需登录会话或 operator token）
        </a-descriptions-item>
        <a-descriptions-item :label="t('settings.configDesc')">
          config.yaml + CONDUCTOR_*（server / database / redis / gateway / ratelimit / auth / credentials / logging / observability）
        </a-descriptions-item>
      </a-descriptions>
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { ensureAuthRequired, invalidateAuthState, isSessionToken } from '@/auth/session'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
const router = useRouter()
const store = useAppStore()
const token = ref(store.token)
const authRequired = ref<boolean | null>(null)

const userTag = computed(() => (isSessionToken(store.token) ? 'session' : 'operator'))

onMounted(async () => {
  authRequired.value = await ensureAuthRequired()
})

function save() {
  store.setToken(token.value.trim())
  message.success(t('settings.savedOk'))
}

async function doLogout() {
  store.clearToken()
  token.value = ''
  invalidateAuthState()
  await router.push('/login')
}
</script>

<style scoped>
.mb {
  margin-bottom: 16px;
}
.session-row {
  display: flex;
  align-items: center;
  gap: 12px;
}
code {
  background: #f5f5f5;
  padding: 2px 6px;
  border-radius: 4px;
}
</style>
