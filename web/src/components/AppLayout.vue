<script setup lang="ts">
import { computed } from 'vue';
import { useI18n } from 'vue-i18n';
import { NLayout, NLayoutSider, NLayoutContent } from 'naive-ui';
import SidebarNav from './SidebarNav.vue';
import SSEIndicator from './SSEIndicator.vue';
import { setLocale } from '@/locales';

const { t, locale } = useI18n();

const currentLocale = computed(() => locale.value as 'pt-BR' | 'en-US');

function toggleLocale() {
  setLocale(currentLocale.value === 'pt-BR' ? 'en-US' : 'pt-BR');
}
</script>

<template>
  <NLayout has-sider style="height: 100vh">
    <NLayoutSider
      :width="240"
      :collapsed-width="0"
      :native-scrollbar="false"
      bordered
      style="background: #1c1c21"
    >
      <SidebarNav />
    </NLayoutSider>

    <NLayoutContent style="background: #101014; overflow-y: auto">
      <div class="layout-header">
        <div class="layout-header-left">
          <slot name="header-title" />
        </div>
        <div class="layout-header-right">
          <SSEIndicator />
          <button class="locale-toggle" @click="toggleLocale">
            {{ t('locale.toggle') }}
          </button>
        </div>
      </div>

      <div class="layout-body">
        <slot />
      </div>
    </NLayoutContent>
  </NLayout>
</template>

<style scoped>
.layout-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 24px;
  border-bottom: 1px solid #2a2a30;
  min-height: 60px;
  box-sizing: border-box;
}

.layout-header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.layout-header-right {
  display: flex;
  align-items: center;
  gap: 16px;
}

.locale-toggle {
  background: none;
  border: 1px solid #3a3a40;
  border-radius: 4px;
  color: #a0a0a0;
  cursor: pointer;
  font-size: 12px;
  font-weight: 600;
  padding: 4px 10px;
  min-height: 28px;
  transition: color 0.15s, border-color 0.15s;
}

.locale-toggle:hover {
  color: #e0e0e0;
  border-color: #5a5a60;
}

.layout-body {
  padding: 24px;
}
</style>
