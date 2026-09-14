// ============ 主题管理 ============
// 每套浅色主题定义完整变量集（主色 + 侧栏 + 顶栏 + 背景），切换视觉差异明显
// 暗黑主题走 html[data-theme="dark"] 覆盖（theme.css），此处只需主色微调

const THEMES = {
  blue: {
    name: '默认蓝', dark: false,
    vars: {
      '--c-primary': '#2563eb', '--c-primary-hover': '#1d4ed8', '--c-primary-weak': '#eff6ff',
      '--c-bg': '#f5f7fb', '--c-card': '#ffffff', '--c-border': '#e5e7eb', '--c-border-strong': '#d1d5db',
      '--c-sidebar-bg': '#ffffff', '--c-sidebar-text': '#3d4450',
      '--c-sidebar-active': '#e8f0fe', '--c-sidebar-active-text': '#1d4ed8',
      '--c-header-bg': '#ffffff', '--c-hover': '#f3f4f6',
      '--c-text': '#1f2329', '--c-text-2': '#646a73', '--c-text-3': '#8f959e'
    }
  },
  green: {
    name: '清新绿', dark: false,
    vars: {
      '--c-primary': '#0e8f4f', '--c-primary-hover': '#0b7a43', '--c-primary-weak': '#e7f6ee',
      '--c-bg': '#f4f9f6', '--c-card': '#ffffff', '--c-border': '#dcebe3', '--c-border-strong': '#bcd9c9',
      '--c-sidebar-bg': '#f0faf4', '--c-sidebar-text': '#2f5340',
      '--c-sidebar-active': '#d3f0df', '--c-sidebar-active-text': '#0b7a43',
      '--c-header-bg': '#ffffff', '--c-hover': '#edf6f0',
      '--c-text': '#1c2b23', '--c-text-2': '#5c7367', '--c-text-3': '#86a094'
    }
  },
  purple: {
    name: '优雅紫', dark: false,
    vars: {
      '--c-primary': '#7c3aed', '--c-primary-hover': '#6d28d9', '--c-primary-weak': '#f3edfd',
      '--c-bg': '#f8f6fc', '--c-card': '#ffffff', '--c-border': '#e6dff2', '--c-border-strong': '#cdbfe3',
      '--c-sidebar-bg': '#f4eefc', '--c-sidebar-text': '#4a3d63',
      '--c-sidebar-active': '#e3d5f7', '--c-sidebar-active-text': '#6d28d9',
      '--c-header-bg': '#ffffff', '--c-hover': '#f1ecf9',
      '--c-text': '#251c38', '--c-text-2': '#6a5d85', '--c-text-3': '#9487b0'
    }
  },
  orange: {
    name: '活力橙', dark: false,
    vars: {
      '--c-primary': '#ea580c', '--c-primary-hover': '#c2410c', '--c-primary-weak': '#fef1e7',
      '--c-bg': '#fbf7f3', '--c-card': '#ffffff', '--c-border': '#f0e2d6', '--c-border-strong': '#e2c7b1',
      '--c-sidebar-bg': '#fef3ec', '--c-sidebar-text': '#5c4636',
      '--c-sidebar-active': '#fbe0cd', '--c-sidebar-active-text': '#c2410c',
      '--c-header-bg': '#ffffff', '--c-hover': '#f7eee6',
      '--c-text': '#33241a', '--c-text-2': '#7a6454', '--c-text-3': '#a08b7c'
    }
  },
  teal: {
    name: '科技青', dark: false,
    vars: {
      '--c-primary': '#0d9488', '--c-primary-hover': '#0f766e', '--c-primary-weak': '#e6f7f5',
      '--c-bg': '#f3f9f9', '--c-card': '#ffffff', '--c-border': '#d8ecea', '--c-border-strong': '#b2d9d5',
      '--c-sidebar-bg': '#eaf7f5', '--c-sidebar-text': '#2e5a56',
      '--c-sidebar-active': '#cdeeea', '--c-sidebar-active-text': '#0f766e',
      '--c-header-bg': '#ffffff', '--c-hover': '#e9f5f4',
      '--c-text': '#18302e', '--c-text-2': '#577572', '--c-text-3': '#809d9a'
    }
  },
  dark: {
    name: '暗黑', dark: true,
    vars: {
      '--c-primary': '#3b82f6', '--c-primary-hover': '#60a5fa', '--c-primary-weak': 'rgba(59,130,246,.15)',
      '--c-bg': '#111418', '--c-card': '#1a1f26', '--c-border': '#2a313a', '--c-border-strong': '#3a424d',
      '--c-sidebar-bg': '#161b21', '--c-sidebar-text': '#aab2bc',
      '--c-sidebar-active': 'rgba(59,130,246,.15)', '--c-sidebar-active-text': '#60a5fa',
      '--c-header-bg': '#161b21', '--c-hover': 'rgba(255,255,255,.06)',
      '--c-text': '#e6e8eb', '--c-text-2': '#9aa3ad', '--c-text-3': '#6b7480'
    }
  }
}

const KEY = 'cs_admin_theme'

// 所有主题出现过的变量键（切换时先清除 inline 残留，避免旧主题值覆盖新主题）
const ALL_KEYS = [...new Set(Object.values(THEMES).flatMap(t => Object.keys(t.vars)))]

function getCurrent() {
  const saved = localStorage.getItem(KEY)
  return THEMES[saved] || THEMES.blue
}

function applyTheme(name) {
  const t = THEMES[name] || THEMES.blue
  const el = document.documentElement
  // 暗色开关（配合 theme.css 的暗色控件样式）
  el.setAttribute('data-theme', t.dark ? 'dark' : 'light')
  // 先清除全部主题变量，再写入当前主题，保证切换无残留
  ALL_KEYS.forEach(k => el.style.removeProperty(k))
  Object.entries(t.vars).forEach(([k, v]) => el.style.setProperty(k, v))
  localStorage.setItem(KEY, name)
}

function initTheme() {
  const saved = localStorage.getItem(KEY)
  applyTheme(THEMES[saved] ? saved : 'blue')
}

export { THEMES, getCurrent, applyTheme, initTheme }
