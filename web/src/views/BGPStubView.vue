<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { NEmpty, NIcon } from 'naive-ui';
import { ServerOutline } from '@vicons/ionicons5';
import AppLayout from '@/components/AppLayout.vue';

const { t } = useI18n();
// No data fetch — permanent stub per D-18 (Phase 1)
</script>

<template>
  <AppLayout>
    <template #header-title>
      <h1 class="page-title">{{ t('bgp.titulo') }}</h1>
    </template>

    <div class="state-center">
      <NEmpty :description="t('bgp.vazio.titulo')">
        <template #icon>
          <NIcon :size="48" :color="'#a0a0a0'">
            <ServerOutline />
          </NIcon>
        </template>
        <template #extra>
          <span class="empty-body">{{ t('bgp.vazio.body') }}</span>
        </template>
      </NEmpty>
    </div>
  </AppLayout>
</template>

<style scoped>
.page-title {
  font-size: 20px;
  font-weight: 600;
  color: #e0e0e0;
  margin: 0;
}

.state-center {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 300px;
}

.empty-body {
  font-size: 13px;
  color: #a0a0a0;
  max-width: 360px;
  text-align: center;
  display: block;
  margin-top: 8px;
}
</style>
