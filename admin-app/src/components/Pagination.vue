<template>
  <div class="pager">
    <span class="text-3" style="font-size:12px;">共 {{ total }} 条</span>
    <button class="btn btn-sm" :disabled="page <= 1" @click="go(page - 1)">‹</button>
    <span class="page-no">{{ page }} / {{ totalPages }}</span>
    <button class="btn btn-sm" :disabled="page >= totalPages" @click="go(page + 1)">›</button>
    <select class="select" style="width:auto;padding:3px 8px;font-size:12px;" :value="size" @change="onSize">
      <option :value="10">10 条/页</option>
      <option :value="20">20 条/页</option>
      <option :value="50">50 条/页</option>
    </select>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  total: { type: Number, default: 0 },
  page: { type: Number, default: 1 },
  size: { type: Number, default: 10 }
})
const emit = defineEmits(['change'])

const totalPages = computed(() => Math.max(1, Math.ceil(props.total / props.size)))

function go(p) {
  if (p < 1 || p > totalPages.value) return
  emit('change', { page: p, size: props.size })
}
function onSize(e) {
  emit('change', { page: 1, size: Number(e.target.value) })
}
</script>

<style scoped>
.pager { display: flex; align-items: center; gap: 8px; justify-content: flex-end; padding-top: 12px; }
.page-no { font-size: 13px; color: var(--c-text-2); min-width: 60px; text-align: center; }
</style>
