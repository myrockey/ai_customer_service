<template>
  <Teleport to="body">
    <div class="toast-wrap">
      <TransitionGroup name="fade">
        <div v-for="t in list" :key="t.id" class="toast-item" :class="'t-' + t.type">
          {{ t.msg }}
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'

const list = ref([])
let seq = 0

function onToast(e) {
  const { msg, type = 'info' } = e.detail || {}
  const id = ++seq
  list.value.push({ id, msg, type })
  setTimeout(() => {
    list.value = list.value.filter(x => x.id !== id)
  }, 2600)
}

onMounted(() => window.addEventListener('cs-toast', onToast))
onUnmounted(() => window.removeEventListener('cs-toast', onToast))
</script>

<style scoped>
.toast-wrap {
  position: fixed; top: 64px; left: 50%; transform: translateX(-50%);
  z-index: 9999; display: flex; flex-direction: column; gap: 8px; align-items: center;
  pointer-events: none;
}
.toast-item {
  padding: 9px 18px; border-radius: 8px; font-size: 13px; color: #fff;
  background: rgba(31,35,41,.88); box-shadow: 0 4px 16px rgba(0,0,0,.15);
  max-width: 480px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.t-success { background: rgba(47,168,79,.92); }
.t-error { background: rgba(229,72,77,.92); }
.t-warning { background: rgba(217,119,6,.92); }
</style>
