<template>
  <div class="theme-sw">
    <span class="text-3" style="font-size:12px;margin-right:4px;">主题</span>
    <div class="swatches">
      <button
        v-for="(t, name) in THEMES" :key="name"
        class="swatch" :class="{ active: current === name }"
        :style="swatchStyle(t)" :title="t.name"
        @click="pick(name)"
      ></button>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { THEMES, applyTheme } from '../theme'

const current = ref(localStorage.getItem('cs_admin_theme') || 'blue')

function swatchStyle(t) {
  if (t.dark) return { background: '#1a1f26', border: '1px solid #3a424d' }
  return { background: t.vars['--c-primary'] }
}
function pick(name) {
  applyTheme(name)
  current.value = name
}
</script>

<style scoped>
.theme-sw { display: flex; align-items: center; }
.swatches { display: flex; gap: 6px; }
.swatch {
  width: 20px; height: 20px; border-radius: 50%; cursor: pointer;
  border: 2px solid transparent; padding: 0; transition: all .15s;
}
.swatch:hover { transform: scale(1.15); }
.swatch.active { border-color: var(--c-text-2); box-shadow: 0 0 0 2px var(--c-card); }
</style>
