<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="visible" class="modal-mask" @click.self="close">
        <div class="modal" :style="{ width: width }">
          <div class="modal-header">
            <span class="modal-title">{{ title }}</span>
            <span class="modal-x" @click="close">✕</span>
          </div>
          <div class="modal-body"><slot /></div>
          <div v-if="$slots.footer" class="modal-footer"><slot name="footer" /></div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup>
defineProps({
  visible: { type: Boolean, default: false },
  title: { type: String, default: '' },
  width: { type: String, default: '560px' }
})
const emit = defineEmits(['close'])
function close() { emit('close') }
</script>

<style scoped>
.modal-mask {
  position: fixed; inset: 0; background: rgba(15,23,42,.5);
  display: flex; align-items: center; justify-content: center; z-index: 1000;
}
.modal {
  background: var(--c-card); border-radius: 12px; max-width: 92vw;
  max-height: 86vh; display: flex; flex-direction: column;
  box-shadow: 0 20px 60px var(--c-shadow);
}
.modal-header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 18px; border-bottom: 1px solid var(--c-border);
}
.modal-title { font-size: 15px; font-weight: 600; }
.modal-x { cursor: pointer; color: var(--c-text-3); font-size: 14px; padding: 2px 6px; }
.modal-x:hover { color: var(--c-text); }
.modal-body { padding: 18px; overflow-y: auto; }
.modal-footer {
  padding: 12px 18px; border-top: 1px solid var(--c-border);
  display: flex; justify-content: flex-end; gap: 10px;
}
.modal-enter-active, .modal-leave-active { transition: opacity .15s; }
.modal-enter-active .modal, .modal-leave-active .modal { transition: transform .15s; }
.modal-enter-from, .modal-leave-to { opacity: 0; }
.modal-enter-from .modal, .modal-leave-to .modal { transform: scale(.96); }
</style>
