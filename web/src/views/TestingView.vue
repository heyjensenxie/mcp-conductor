<template>
  <div>
    <a-card :bordered="true" class="mb">
      <a-space wrap>
        <a-select v-model:value="serverId" :placeholder="t('testing.selectPlaceholder')" style="width: 320px" @change="onSelectServer">
          <a-select-option v-for="s in servers" :key="s.id" :value="s.id">{{ s.name }} ({{ s.endpoint }})</a-select-option>
        </a-select>
        <a-button type="primary" :disabled="!serverId" :loading="testing" @click="connect">{{ t('testing.testConnection') }}</a-button>
      </a-space>
    </a-card>

    <ToolInvoker v-if="tools.length" :tools="tools" />
    <a-empty v-else-if="serverId && !loadingTools" :description="t('testing.empty')" />
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { listServers, listServerTools, testServer } from '@/api'
import type { MCPServer, Tool } from '@/types'
import ToolInvoker from '@/components/ToolInvoker.vue'

const { t } = useI18n()
const servers = ref<MCPServer[]>([])
const serverId = ref('')
const tools = ref<Tool[]>([])
const testing = ref(false)
const loadingTools = ref(false)

onMounted(async () => {
  try {
    servers.value = await listServers()
  } catch (e) {
    message.error(String(e))
  }
})

function onSelectServer() {
  tools.value = []
}

async function connect() {
  if (!serverId.value) return
  testing.value = true
  loadingTools.value = true
  try {
    await testServer(serverId.value)
    tools.value = await listServerTools(serverId.value)
    message.success(t('testing.connectedOk'))
  } catch (e) {
    message.error(`${t('testing.connectFail')}: ${e}`)
  } finally {
    testing.value = false
    loadingTools.value = false
  }
}
</script>

<style scoped>
.mb {
  margin-bottom: 16px;
}
</style>
