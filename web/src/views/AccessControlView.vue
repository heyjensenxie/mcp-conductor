<template>
  <div>
    <div class="toolbar">
      <a-button type="primary" @click="dialogVisible = true">
        <template #icon><PlusOutlined /></template>{{ t('access.newPolicy') }}
      </a-button>
    </div>

    <a-table :data-source="policies" :columns="columns" :loading="loading" :pagination="false" :row-key="(r: any) => r.id">
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'rules'">
          <a-tag v-for="rule in record.rules" :key="rule.tool + rule.subject" :color="rule.effect === 'deny' ? 'red' : 'green'" class="rule-tag">
            {{ rule.subject }} → {{ rule.tool }} [{{ rule.effect }}]
          </a-tag>
        </template>
        <template v-else-if="column.key === 'enabled'">
          <a-tag :color="record.enabled ? 'green' : 'default'">{{ record.enabled ? t('common.yes') : t('common.no') }}</a-tag>
        </template>
      </template>
    </a-table>

    <a-modal v-model:open="dialogVisible" :title="t('access.newPolicy')" :ok-text="t('access.create')" :cancel-text="t('common.cancel')" width="600px" @ok="submit">
      <a-alert type="info" show-icon class="mb">{{ t('access.formatHint') }}</a-alert>
      <a-form :label-col="{ span: 4 }" :wrapper-col="{ span: 20 }">
        <a-form-item :label="t('access.name')" :required="true">
          <a-input v-model:value="form.name" />
        </a-form-item>
        <a-form-item :label="t('access.rules')">
          <a-textarea v-model:value="rulesText" :rows="5" placeholder="agent-a|payment.create|deny" />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { PlusOutlined } from '@ant-design/icons-vue'
import { createPolicy, listPolicies } from '@/api'
import type { Policy, PolicyRule } from '@/types'

const { t } = useI18n()
const policies = ref<Policy[]>([])
const loading = ref(false)
const dialogVisible = ref(false)
const rulesText = ref('')
const form = reactive({ name: '' })

const columns = computed<any[]>(() => [
  { title: t('access.name'), key: 'name', dataIndex: 'name' },
  { title: t('access.rules'), key: 'rules' },
  { title: t('access.enabled'), key: 'enabled', dataIndex: 'enabled', width: 100 },
])

onMounted(load)

async function load() {
  loading.value = true
  try {
    policies.value = await listPolicies()
  } catch (e) {
    message.error(String(e))
  } finally {
    loading.value = false
  }
}

async function submit() {
  if (!form.name) {
    message.warning(t('access.nameRequired'))
    return
  }
  const rules: PolicyRule[] = rulesText.value
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [subject, tool, effect] = line.split('|').map((s) => s.trim())
      return { subject, tool, effect: (effect === 'deny' ? 'deny' : 'allow') as PolicyRule['effect'] }
    })
  try {
    await createPolicy({ ...form, enabled: true, rules })
    message.success(t('access.createdOk'))
    dialogVisible.value = false
    await load()
  } catch (e) {
    message.error(String(e))
  }
}
</script>

<style scoped>
.toolbar {
  margin-bottom: 16px;
}
.rule-tag {
  margin-right: 6px;
}
.mb {
  margin-bottom: 12px;
}
</style>