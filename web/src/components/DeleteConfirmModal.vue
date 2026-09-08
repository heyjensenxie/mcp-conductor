<template>
  <a-modal
    :open="open"
    :title="title"
    :ok-text="confirmText"
    :cancel-text="cancelText"
    :confirm-loading="loading"
    :ok-button-props="{ danger: true, disabled: requiresTarget && typedTarget !== target }"
    @update:open="emit('update:open', $event)"
    @ok="emit('confirm')"
  >
    <a-alert type="error" show-icon :message="description" class="delete-warning" />
    <p v-if="target" class="delete-target">{{ target }}</p>
    <template v-if="requiresTarget">
      <p class="delete-instruction">{{ typingHint }}</p>
      <a-input v-model:value="typedTarget" :placeholder="target" autocomplete="off" />
    </template>
  </a-modal>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'

const props = withDefaults(defineProps<{
  open: boolean
  title: string
  description: string
  target?: string
  requiresTarget?: boolean
  typingHint?: string
  confirmText: string
  cancelText: string
  loading?: boolean
}>(), {
  target: '',
  requiresTarget: false,
  typingHint: '',
  loading: false,
})

const emit = defineEmits<{
  'update:open': [open: boolean]
  confirm: []
}>()

const typedTarget = ref('')

// 每次重新打开都要求重新确认，避免关闭后残留已匹配的输入。
watch(() => props.open, (open) => {
  if (open) typedTarget.value = ''
})
</script>

<style scoped>
.delete-warning { margin-bottom: 12px; }
.delete-target {
  margin: 0 0 12px;
  padding: 8px 10px;
  border: 1px solid #ffccc7;
  border-radius: 6px;
  background: #fff2f0;
  color: #a8071a;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  overflow-wrap: anywhere;
}
.delete-instruction { margin: 0 0 8px; color: rgba(0, 0, 0, 0.65); }
</style>
