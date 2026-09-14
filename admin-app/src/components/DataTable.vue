<template>
  <div class="dtable card">
    <div v-if="$slots.toolbar" class="dtable-toolbar"><slot name="toolbar" /></div>
    <div class="dtable-scroll">
      <table class="dtable-main">
        <thead>
          <tr>
            <th v-for="c in columns" :key="c.key" :style="thStyle(c)" v-html="c.title"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, i) in rows" :key="rowKey(row, i)" :class="{ 'row-click': rowClickable }" @click="onRowClick(row)">
            <td v-for="c in columns" :key="c.key" :style="tdStyle(c)">
              <slot :name="'cell-' + c.key" :row="row" :value="row[c.key]">
                <span v-if="c.render" v-html="c.render(row)"></span>
                <span v-else>{{ fmt(row[c.key], c) }}</span>
              </slot>
            </td>
          </tr>
          <tr v-if="!rows.length">
            <td :colspan="columns.length" style="text-align:center;color:var(--c-text-3);padding:36px 0;">
              暂无数据
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-if="$slots.pager" class="dtable-pager"><slot name="pager" /></div>
  </div>
</template>

<script setup>
const props = defineProps({
  columns: { type: Array, required: true },   // [{key,title,width,align,render}]
  rows: { type: Array, default: () => [] },
  rowKey: { type: Function, default: (r, i) => i },
  rowClickable: { type: Boolean, default: false }
})
const emit = defineEmits(['row-click'])

function thStyle(c) {
  const s = {}
  if (c.width) s.width = c.width
  if (c.align) s.textAlign = c.align
  return s
}
function tdStyle(c) {
  if (c.align) return { textAlign: c.align }
  return {}
}
function fmt(v, c) {
  if (c.formatter) return c.formatter(v)
  if (v === null || v === undefined || v === '') return '-'
  return v
}
function onRowClick(row) {
  if (props.rowClickable) emit('row-click', row)
}
</script>

<style scoped>
.dtable-toolbar { display: flex; align-items: center; gap: 10px; margin-bottom: 14px; flex-wrap: wrap; }
.dtable-scroll { overflow-x: auto; }
.dtable-main { width: 100%; border-collapse: collapse; font-size: 13px; }
.dtable-main th {
  text-align: left; padding: 9px 12px; background: var(--c-bg);
  color: var(--c-text-2); font-weight: 500; border-bottom: 1px solid var(--c-border);
  white-space: nowrap;
}
.dtable-main td { padding: 10px 12px; border-bottom: 1px solid var(--c-border); color: var(--c-text); }
.dtable-main tbody tr:hover { background: var(--c-hover); }
.row-click { cursor: pointer; }
.dtable-pager { border-top: 1px solid var(--c-border); margin-top: 12px; }
</style>
