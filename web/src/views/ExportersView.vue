<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue';
import { useI18n } from 'vue-i18n';
import { NAlert, NSpin, NEmpty, NTag, NButton } from 'naive-ui';
import AppLayout from '@/components/AppLayout.vue';
import { api } from '@/api/client';

const { t } = useI18n();

interface Exporter {
  source_ip: string;
  type: string;
  last_seen: string | null;
  flows_per_sec: number;
  sample_rate_override: number;
  status: 'online' | 'stale' | 'offline';
}

const exporters = ref<Exporter[]>([]);
const loading = ref(true);
const error = ref<string | null>(null);

const warmingCount = computed(() =>
  exporters.value.filter((e) => e.last_seen === null).length
);

function statusColor(status: string): string {
  if (status === 'online') return '#18a058';
  if (status === 'stale') return '#f0a020';
  return '#d03050';
}

function statusLabel(status: string): string {
  if (status === 'online') return t('exporters.status.online');
  if (status === 'stale') return t('exporters.status.stale');
  return t('exporters.status.offline');
}

function formatRelative(lastSeen: string | null): string {
  if (!lastSeen) return '—';
  const secs = Math.floor((Date.now() - new Date(lastSeen).getTime()) / 1000);
  if (secs < 60) return 'há ' + secs + 's';
  if (secs < 3600) return 'há ' + Math.floor(secs / 60) + 'min';
  return 'há ' + Math.floor(secs / 3600) + 'h';
}

async function loadExporters() {
  error.value = null;
  try {
    const res = await api<{ items: Exporter[] }>('/api/exporters');
    exporters.value = res.items;
  } catch {
    error.value = 'error';
  } finally {
    loading.value = false;
  }
}

async function retry() {
  loading.value = true;
  await loadExporters();
}

let intervalId: ReturnType<typeof setInterval> | null = null;

onMounted(async () => {
  await loadExporters();
  intervalId = setInterval(loadExporters, 15000);
});

onUnmounted(() => {
  if (intervalId) clearInterval(intervalId);
});
</script>

<template>
  <AppLayout>
    <template #header-title>
      <h1 class="page-title">{{ t('exporters.titulo') }}</h1>
    </template>

    <!-- Warming alert -->
    <NAlert
      v-if="warmingCount > 0"
      type="warning"
      style="margin-bottom: 16px"
    >
      {{ t('exporters.warming', { count: warmingCount }) }}
    </NAlert>

    <!-- Loading state -->
    <div v-if="loading" class="state-center">
      <NSpin size="large" />
    </div>

    <!-- Error state -->
    <NAlert v-else-if="error" type="error" style="margin-bottom: 16px">
      <template #default>
        <NButton text @click="retry">{{ t('actions.retry') }}</NButton>
      </template>
    </NAlert>

    <!-- Empty state -->
    <div v-else-if="exporters.length === 0" class="state-center">
      <NEmpty :description="t('exporters.vazio.titulo')">
        <template #extra>
          <span class="empty-body">{{ t('exporters.vazio.body') }}</span>
        </template>
      </NEmpty>
    </div>

    <!-- Exporter table -->
    <div v-else class="table-wrapper">
      <table class="data-table">
        <thead>
          <tr>
            <th>{{ t('exporters.col.ip') }}</th>
            <th>{{ t('exporters.col.tipo') }}</th>
            <th>{{ t('exporters.col.ultimo_flow') }}</th>
            <th class="right">{{ t('exporters.col.taxa') }}</th>
            <th>{{ t('exporters.col.override') }}</th>
            <th>{{ t('exporters.col.status') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="exp in exporters" :key="exp.source_ip">
            <td class="mono">{{ exp.source_ip }}</td>
            <td>
              <NTag size="small" round>{{ exp.type }}</NTag>
            </td>
            <td style="color: #a0a0a0">{{ formatRelative(exp.last_seen) }}</td>
            <td class="mono right">{{ exp.flows_per_sec.toLocaleString() }} f/s</td>
            <td>{{ exp.sample_rate_override > 0 ? exp.sample_rate_override : '—' }}</td>
            <td>
              <div class="status-cell">
                <span
                  class="status-dot"
                  :style="{ backgroundColor: statusColor(exp.status) }"
                />
                <span>{{ statusLabel(exp.status) }}</span>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
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
  min-height: 200px;
}

.empty-body {
  font-size: 13px;
  color: #a0a0a0;
  max-width: 400px;
  text-align: center;
  display: block;
  margin-top: 8px;
}

.table-wrapper {
  overflow-x: auto;
  border-radius: 8px;
  background: #1c1c21;
}

.data-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 14px;
}

.data-table th {
  padding: 10px 14px;
  font-size: 12px;
  font-weight: 600;
  color: #a0a0a0;
  text-align: left;
  border-bottom: 1px solid #2a2a30;
  white-space: nowrap;
}

.data-table td {
  padding: 10px 14px;
  border-bottom: 1px solid #1e1e24;
  color: #e0e0e0;
  vertical-align: middle;
}

.data-table tr:hover td {
  background-color: rgba(255, 255, 255, 0.03);
}

.mono {
  font-family: 'JetBrains Mono', 'Fira Code', ui-monospace, monospace;
}

.right {
  text-align: right;
}

.status-cell {
  display: flex;
  align-items: center;
  gap: 6px;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
</style>
