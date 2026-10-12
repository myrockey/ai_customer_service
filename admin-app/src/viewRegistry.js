// 页面组件注册表：枚举 src/views 下全部 .vue 组件（供动态路由与菜单「组件」下拉共用）
// 与 router.js 的 import.meta.glob('./views/**/*.vue') 同一套枚举，保证下拉可选项=实际可路由组件
// 注意：glob 模式必须相对本文件（src/）解析，跨层相对路径（../views/）在部分 vite 版本会匹配为空
const viewModules = import.meta.glob('./views/**/*.vue')

// 组件名形如 views/Ops.vue（与 menus.component 存值一致）
export const viewOptions = Object.keys(viewModules)
  .map(k => k.replace(/^\.\//, ''))
  .sort((a, b) => a.localeCompare(b))
