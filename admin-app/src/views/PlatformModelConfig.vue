<template>
  <div>
    <div class="notice-bar">
      💡 平台默认模型 = 平台管理员租户（t_admin）的模型配置，作为所有租户的默认值。
      租户在「租户模型配置」中选择「跟随平台」即继承此配置；也可选择自定义（BYOK）覆盖。
      模型提供商（名称 / 请求地址）统一维护在「模型提供商」中；API Key 为本租户密钥，AES 加密存于 t_admin 配置。
      修改保存后 Agent 会自动重建，新会话立即生效。
    </div>
    <!-- 平台默认模型配置 = 平台管理员租户 t_admin 的模型配置（单数据源） -->
    <TenantModelConfig :platform="true" />
  </div>
</template>

<script setup>
import TenantModelConfig from './TenantModelConfig.vue'
</script>

<style scoped>
.notice-bar {
  background: var(--c-primary-weak); color: var(--c-primary); border: 1px solid var(--c-border);
  border-radius: var(--radius); padding: 10px 14px; font-size: 13px;
}
</style>
