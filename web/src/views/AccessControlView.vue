<template>
  <a-spin :spinning="loading">
        <section class="access-hero">
          <div>
            <p class="eyebrow">DATA PLANE · API KEYS</p>
            <h2>{{ t('access.keyWorkspaceTitle') }}</h2>
            <p>{{ t('access.keyWorkspaceDesc') }}</p>
          </div>
          <a-button type="primary" size="large" @click="openCreate"><template #icon><PlusOutlined /></template>{{ t('access.newKey') }}</a-button>
        </section>
        <a-alert v-if="authRequired === false" type="warning" show-icon class="mb" :message="t('access.authDisabled')" :description="t('access.authDisabledDesc')" />
        <div class="filters">
          <a-space wrap :size="8">
            <a-input v-model:value="filters.q" allow-clear :placeholder="t('filter.keyword')" style="width: 240px" @press-enter="onFilterChange">
              <template #prefix><SearchOutlined /></template>
            </a-input>
            <a-select v-model:value="filters.enabled" allow-clear :placeholder="t('filter.statusPlaceholder')" style="width: 120px" @change="onFilterChange">
              <a-select-option value="true">{{ t('filter.enabled') }}</a-select-option>
              <a-select-option value="false">{{ t('filter.disabled') }}</a-select-option>
            </a-select>
          </a-space>
        </div>
        <a-empty v-if="!keysLoading && keys.length === 0 && pagination.total === 0" :description="t('access.noKeysDesc')" />
        <a-table v-else :data-source="keys" :columns="keyColumns" :loading="keysLoading" :pagination="pagination" @change="onTableChange" :row-key="(r: AccessKey) => r.id">
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'enabled'"><a-badge :status="record.enabled ? 'success' : 'default'" :text="record.enabled ? t('access.active') : t('access.inactive')" /></template>
            <template v-else-if="column.key === 'quota'"><span class="mono">{{ record.qps > 0 ? `${record.qps}/s · ${record.burst}` : t('access.globalQuota') }}</span></template>
            <template v-else-if="column.key === 'tools_count'"><a-tag color="blue">{{ record.grants?.length ?? 0 }} {{ t('access.grantsUnit') }}</a-tag></template>
            <template v-else-if="column.key === 'actions'"><a-space><a-button type="link" @click="openDetail(record.id)">{{ t('access.openWorkspace') }}</a-button><a-popconfirm :title="t('common.confirmDelete')" @confirm="remove(record)"><a-button type="link" danger>{{ t('common.delete') }}</a-button></a-popconfirm></a-space></template>
          </template>
        </a-table>

    <a-modal v-model:open="createVisible" :title="t('access.newKey')" :ok-text="t('common.create')" :confirm-loading="creating" @ok="submitCreate">
      <a-form :label-col="{ span: 6 }" :wrapper-col="{ span: 18 }">
        <a-form-item :label="t('access.name')" required><a-input v-model:value="createForm.name" /></a-form-item>
        <a-form-item :label="t('access.subject')" required><a-input v-model:value="createForm.subject" placeholder="partner-a" /></a-form-item>
        <a-form-item :label="t('access.qps')"><a-input-number v-model:value="createForm.qps" :min="0" :style="{ width: '100%' }" :placeholder="t('access.qupPlaceholder')" /></a-form-item>
        <a-form-item :label="t('access.burst')"><a-input-number v-model:value="createForm.burst" :min="0" :style="{ width: '100%' }" :placeholder="t('access.burstPlaceholder')" /></a-form-item>
      </a-form>
    </a-modal>
    <a-modal v-model:open="secretVisible" :title="t('access.secret')" :footer="null" width="560px">
      <a-alert type="warning" show-icon class="mb" :message="t('access.secretOnce')" />
      <a-input-group compact class="secret-row"><a-input :value="createdSecret" read-only class="mono" /><a-button @click="copySecret"><template #icon><CopyOutlined /></template>{{ t('access.copy') }}</a-button></a-input-group>
      <a-button block type="primary" class="configure-btn" @click="openCreatedDetail">{{ t('access.configureNow') }}</a-button>
    </a-modal>
  </a-spin>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { CopyOutlined, PlusOutlined, SearchOutlined } from '@ant-design/icons-vue'
import { createKey, deleteKey, getAuthStatus, listKeys } from '@/api'
import type { AccessKey } from '@/types'

const { t } = useI18n()
const router = useRouter()
const loading = ref(false)
const keysLoading = ref(false)
const keys = ref<AccessKey[]>([])
const pagination = reactive({
  current: 1,
  pageSize: 20,
  total: 0,
  showSizeChanger: true,
  showTotal: (total: number) => t('filter.total', { total }),
})
// enabled 下拉初始为 undefined：antd Select 仅在值为空(null/undefined)时展示占位文案。
const filters = reactive<{ q: string; enabled?: string }>({ q: '' })
const authRequired = ref<boolean | null>(null)
const createVisible = ref(false)
const creating = ref(false)
const secretVisible = ref(false)
const createdSecret = ref('')
const createdKeyID = ref('')
const createForm = reactive({ name: '', subject: '', qps: 0, burst: 0 })

const keyColumns = computed<any[]>(() => [
  { title: t('access.name'), key: 'name', dataIndex: 'name' },
  { title: t('access.subject'), key: 'subject', dataIndex: 'subject' },
  { title: t('access.qps').split(' ')[0], key: 'quota', width: 150 },
  { title: t('access.toolsCount'), key: 'tools_count', width: 105 },
  { title: t('access.enabled'), key: 'enabled', width: 105 },
  { title: t('access.actions'), key: 'actions', width: 190 },
])

function openCreate() { Object.assign(createForm, { name: '', subject: '', qps: 0, burst: 0 }); createVisible.value = true }
function openDetail(id: string) { router.push({ name: 'access-key-detail', params: { id } }) }
function openCreatedDetail() { secretVisible.value = false; openDetail(createdKeyID.value) }

async function loadKeys() {
  keysLoading.value = true
  try {
    const res = await listKeys({
      page: pagination.current,
      page_size: pagination.pageSize,
      q: filters.q.trim() || undefined,
      enabled: filters.enabled === 'true' ? true : filters.enabled === 'false' ? false : undefined,
    })
    keys.value = res.items
    pagination.total = res.total
    if (res.items.length === 0 && pagination.current > 1 && res.total > 0) {
      pagination.current -= 1
      await loadKeys()
      return
    }
  } catch (e) {
    message.error(String(e))
  } finally {
    keysLoading.value = false
  }
}

function onFilterChange() {
  pagination.current = 1
  void loadKeys()
}

function onTableChange(p: { current?: number; pageSize?: number }) {
  if (p.current) pagination.current = p.current
  if (p.pageSize && p.pageSize !== pagination.pageSize) {
    pagination.pageSize = p.pageSize
    pagination.current = 1
  }
  void loadKeys()
}

async function submitCreate() {
  if (!createForm.name || !createForm.subject) { message.warning(t('access.subjectRequired')); return }
  creating.value = true
  try {
    const key = await createKey({ ...createForm, grants: [] })
    createdSecret.value = key.secret ?? ''; createdKeyID.value = key.id; createVisible.value = false; secretVisible.value = true
    pagination.current = 1
    await loadKeys()
  } catch (e) { message.error(String(e)) } finally { creating.value = false }
}
async function remove(key: AccessKey) { try { await deleteKey(key.id); pagination.current = 1; await loadKeys(); message.success(t('access.deletedOk')) } catch (e) { message.error(String(e)) } }
async function copySecret() { try { await navigator.clipboard.writeText(createdSecret.value); message.success(t('access.copied')) } catch { message.warning(t('access.copyFailed')) } }

onMounted(async () => { loading.value = true; try { await Promise.all([loadKeys(), getAuthStatus().then((s) => { authRequired.value = s.auth_required })]) } finally { loading.value = false } })
</script>

<style scoped>
.access-hero { display: flex; justify-content: space-between; align-items: flex-end; margin: 2px 0 22px; padding: 25px 28px; border: 1px solid #d9e5f5; border-radius: 12px; background: linear-gradient(112deg, #f7fbff, #eef5ff 58%, #f9fcff); }
.access-hero h2 { margin: 2px 0 5px; font-size: 24px; color: #102a43; }
.access-hero p { margin: 0; color: #627d98; max-width: 620px; }
.eyebrow { color: #1677ff !important; font-family: var(--mc-mono); font-size: 11px; letter-spacing: .12em; }
.mb { margin-bottom: 16px; }
.filters { margin: 0 0 16px; }
.secret-row { display: flex; }
.configure-btn { margin-top: 18px; }
.mono { font-family: var(--mc-mono); }
</style>
