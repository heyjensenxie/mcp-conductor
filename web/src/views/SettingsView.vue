<template>
  <div>
    <a-card :bordered="true" title="API Credential" class="mb">
      <a-row :gutter="16">
        <a-col :span="10">
          <a-input v-model:value="token" type="password" placeholder="后端启用 auth 时填入 X-Api-Key 凭据" />
        </a-col>
        <a-col>
          <a-button type="primary" @click="save">Save</a-button>
        </a-col>
      </a-row>
    </a-card>

    <a-card :bordered="true" title="About" class="mb">
      <a-descriptions :column="1" bordered size="small">
        <a-descriptions-item label="Version">0.1.0</a-descriptions-item>
        <a-descriptions-item label="MCP Endpoint">
          <code>/mcp</code>（streamable HTTP 无状态模式：tools/list / tools/call）
        </a-descriptions-item>
        <a-descriptions-item label="Control API">
          <code>/api/*</code>（统一信封：code / message / request_id）
        </a-descriptions-item>
        <a-descriptions-item label="Config">
          config.yaml + 环境变量（CONDUCTOR_*，优先级更高）；分组 server / database / redis / gateway / ratelimit / auth / logging / observability
        </a-descriptions-item>
      </a-descriptions>
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { message } from 'ant-design-vue'
import { useAppStore } from '@/stores/app'

const store = useAppStore()
const token = ref(store.token)

function save() {
  store.setToken(token.value.trim())
  message.success('凭据已保存，后续请求将携带')
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