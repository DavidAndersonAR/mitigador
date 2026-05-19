<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { NEmpty } from 'naive-ui';
import AppLayout from '@/components/AppLayout.vue';

const { t } = useI18n();
</script>

<template>
  <AppLayout>
    <template #header-title>
      <h1 class="page-title">{{ t('traffic.title') }}</h1>
    </template>

    <!-- Placeholder body — fully replaced by plan 05. Keep AppLayout wiring so the route renders and i18n works. -->
    <NEmpty :description="t('traffic.empty.title')">
      <template #extra>
        <span class="empty-body">{{ t('traffic.empty.body') }}</span>
      </template>
    </NEmpty>
  </AppLayout>
</template>

<style scoped>
.page-title {
  font-size: 20px;
  font-weight: 600;
  color: #e0e0e0;
  margin: 0;
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
