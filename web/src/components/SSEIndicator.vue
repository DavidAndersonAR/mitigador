<script setup lang="ts">
import { computed } from 'vue';
import { useI18n } from 'vue-i18n';
import { useIncidentsStore } from '@/stores/incidents';

const { t } = useI18n();
const incidents = useIncidentsStore();

const dotColor = computed(() => {
  switch (incidents.sseStatus) {
    case 'open':
      return '#18a058'; // green — connected
    case 'connecting':
    case 'reconnecting':
      return '#f0a020'; // amber — reconnecting
    case 'closed':
    default:
      return '#d03050'; // red — disconnected
  }
});

const label = computed(() => {
  switch (incidents.sseStatus) {
    case 'open':
      return '';
    case 'connecting':
    case 'reconnecting':
      return t('sse.reconectando');
    case 'closed':
    default:
      return t('sse.desconectado');
  }
});
</script>

<template>
  <div class="sse-indicator" :data-status="incidents.sseStatus">
    <span
      class="sse-dot"
      :style="{ backgroundColor: dotColor }"
      :title="label || t('sse.conectado')"
    />
    <span v-if="label" class="sse-label">{{ label }}</span>
  </div>
</template>

<style scoped>
.sse-indicator {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #a0a0a0;
}

.sse-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.sse-label {
  white-space: nowrap;
}
</style>
