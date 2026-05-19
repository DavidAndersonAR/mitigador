<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue';
import { useI18n } from 'vue-i18n';
import { NCard, NEmpty, NSpin, NAlert, NButton, NTag, NProgress } from 'naive-ui';
import AppLayout from '@/components/AppLayout.vue';
import { useIncidentsStore, type ActiveAttack } from '@/stores/incidents';
import { connectEvents } from '@/api/sse';
import { api } from '@/api/client';

const { t } = useI18n();
const incidents = useIncidentsStore();

// Summary strip counts
const exportersOnline = ref(0);
const alertsToday = ref(0);
const loadingSnapshot = ref(true);
const snapshotError = ref<string | null>(null);

// Per-row flash/fade state
const flashingRows = ref<Set<string>>(new Set());
const fadingRows = ref<Set<string>>(new Set());
const fadeTimers = new Map<string, ReturnType<typeof setTimeout>>();

let disconnectSSE: (() => void) | null = null;

const activeRows = computed(() =>
  incidents.active.filter((a) => !a.ended || fadingRows.value.has(a.incident_id))
);

function rowStyle(row: ActiveAttack) {
  const styles: Record<string, string> = {};
  if (row.confidence >= 80) {
    styles['border-left'] = '3px solid #d03050';
  } else if (row.confidence >= 40) {
    styles['border-left'] = '3px solid #f0a020';
  } else {
    styles['border-left'] = '3px solid transparent';
  }
  if (fadingRows.value.has(row.incident_id)) {
    styles['opacity'] = '0.3';
    styles['text-decoration'] = 'line-through';
    styles['transition'] = 'opacity 0.5s';
  }
  if (flashingRows.value.has(row.incident_id)) {
    styles['background-color'] = 'rgba(24, 160, 88, 0.12)';
    styles['transition'] = 'background-color 1s';
  }
  return styles;
}

function formatBps(bps: number): string {
  if (bps >= 1e9) return (bps / 1e9).toFixed(1) + ' Gbps';
  if (bps >= 1e6) return (bps / 1e6).toFixed(1) + ' Mbps';
  if (bps >= 1e3) return (bps / 1e3).toFixed(1) + ' Kbps';
  return bps + ' bps';
}

function formatPps(pps: number): string {
  if (pps >= 1e6) return (pps / 1e6).toFixed(1) + 'M';
  if (pps >= 1e3) return (pps / 1e3).toFixed(1) + 'K';
  return String(pps);
}

function formatDuration(startedAt: string): string {
  const secs = Math.floor((Date.now() - new Date(startedAt).getTime()) / 1000);
  if (secs < 60) return secs + 's';
  if (secs < 3600) return Math.floor(secs / 60) + 'm ' + (secs % 60) + 's';
  return Math.floor(secs / 3600) + 'h ' + Math.floor((secs % 3600) / 60) + 'm';
}

function formatRelative(startedAt: string): string {
  const secs = Math.floor((Date.now() - new Date(startedAt).getTime()) / 1000);
  if (secs < 60) return 'há ' + secs + 's';
  if (secs < 3600) return 'há ' + Math.floor(secs / 60) + 'min';
  return 'há ' + Math.floor(secs / 3600) + 'h';
}

function vectorLabel(vector: string): string {
  if (vector === 'udp_flood') return t('vetor.udp_flood');
  if (vector === 'icmp_flood') return t('vetor.icmp_flood');
  return vector;
}

function onSSEOpen() {
  incidents.sseStatus = 'open';
}

function onSSEError() {
  incidents.sseStatus = 'reconnecting';
}

function onSSEMessage(type: string, data: unknown) {
  const ev = data as Parameters<typeof incidents.handleEvent>[1];
  incidents.handleEvent(type, ev);

  const incidentId = ev?.incident_id;
  if (!incidentId) return;

  if (type === 'attack.started') {
    flashingRows.value.add(incidentId);
    setTimeout(() => {
      flashingRows.value.delete(incidentId);
    }, 1000);
  } else if (type === 'attack.ended') {
    fadingRows.value.add(incidentId);
    const timer = setTimeout(() => {
      fadingRows.value.delete(incidentId);
      // Remove from store active list
      const idx = incidents.active.findIndex((a) => a.incident_id === incidentId);
      if (idx >= 0) incidents.active.splice(idx, 1);
    }, 10000);
    fadeTimers.set(incidentId, timer);
  }
}

async function loadSummary() {
  loadingSnapshot.value = true;
  snapshotError.value = null;
  try {
    await incidents.loadActiveSnapshot();

    // Exporters online count
    const expRes = await api<{ items: Array<{ status: string }> }>('/api/exporters');
    exportersOnline.value = expRes.items.filter((e) => e.status === 'online').length;

    // Alerts today count
    const midnight = new Date();
    midnight.setHours(0, 0, 0, 0);
    const incRes = await api<{ total: number }>(`/api/incidents?since=${midnight.toISOString()}&limit=1`);
    alertsToday.value = incRes.total;
  } catch {
    snapshotError.value = 'error';
  } finally {
    loadingSnapshot.value = false;
  }
}

async function retry() {
  await loadSummary();
}

// Ticker for live duration display
let durationTicker: ReturnType<typeof setInterval> | null = null;
const tickCount = ref(0); // force reactive update

onMounted(async () => {
  incidents.sseStatus = 'connecting';
  await loadSummary();

  disconnectSSE = connectEvents({
    onOpen: onSSEOpen,
    onMessage: onSSEMessage,
    onError: onSSEError,
  });

  durationTicker = setInterval(() => {
    tickCount.value++;
  }, 5000);
});

onUnmounted(() => {
  if (disconnectSSE) disconnectSSE();
  if (durationTicker) clearInterval(durationTicker);
  for (const timer of fadeTimers.values()) clearTimeout(timer);
  fadeTimers.clear();
});
</script>

<template>
  <AppLayout>
    <template #header-title>
      <h1 class="page-title">{{ t('ataques.titulo') }}</h1>
    </template>

    <!-- Summary strip -->
    <div class="summary-strip">
      <NCard class="summary-card" :bordered="false">
        <div class="summary-value" style="font-size: 28px; font-weight: 600">
          {{ incidents.activeCount }}
        </div>
        <div class="summary-label">{{ t('ataques.ativos') }}</div>
      </NCard>
      <NCard class="summary-card" :bordered="false">
        <div class="summary-value">{{ exportersOnline }}</div>
        <div class="summary-label">{{ t('ataques.exporters_online') }}</div>
      </NCard>
      <NCard class="summary-card" :bordered="false">
        <div class="summary-value">{{ alertsToday }}</div>
        <div class="summary-label">{{ t('ataques.alertas_hoje') }}</div>
      </NCard>
    </div>

    <!-- Loading state -->
    <div v-if="loadingSnapshot" class="state-center">
      <NSpin size="large" />
    </div>

    <!-- Error state -->
    <NAlert v-else-if="snapshotError" type="error" style="margin-bottom: 16px">
      <template #default>
        <NButton text @click="retry">{{ t('actions.retry') }}</NButton>
      </template>
    </NAlert>

    <!-- Attack table -->
    <template v-else>
      <!-- Empty state -->
      <div v-if="activeRows.length === 0" class="state-center">
        <NEmpty :description="t('ataques.vazio.titulo')">
          <template #extra>
            <span class="empty-body">{{ t('ataques.vazio.body') }}</span>
          </template>
        </NEmpty>
      </div>

      <!-- Attack rows -->
      <div v-else class="attack-table-wrapper">
        <table class="attack-table">
          <thead>
            <tr>
              <th>{{ t('ataques.col.ip') }}</th>
              <th>{{ t('ataques.col.vetor') }}</th>
              <th class="right">{{ t('ataques.col.pps') }}</th>
              <th class="right">{{ t('ataques.col.bps') }}</th>
              <th>{{ t('ataques.col.duracao') }}</th>
              <th>{{ t('ataques.col.score') }}</th>
              <th>{{ t('ataques.col.iniciado') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="row in activeRows"
              :key="row.incident_id"
              :style="rowStyle(row)"
              class="attack-row"
            >
              <td class="mono">{{ row.host_ip }}</td>
              <td>
                <NTag :type="row.vector === 'udp_flood' ? 'error' : 'warning'" size="small" round>
                  {{ vectorLabel(row.vector) }}
                </NTag>
              </td>
              <td class="mono right">{{ formatPps(row.pps) }}</td>
              <td class="mono right">{{ formatBps(row.bps) }}</td>
              <td>{{ formatDuration(row.started_at) }}</td>
              <td>
                <div style="display: flex; align-items: center; gap: 6px">
                  <NProgress
                    type="line"
                    :percentage="row.confidence"
                    :height="6"
                    :show-indicator="false"
                    :color="row.confidence >= 80 ? '#d03050' : row.confidence >= 40 ? '#f0a020' : '#18a058'"
                    style="width: 60px"
                  />
                  <span style="font-size: 12px; color: #a0a0a0">{{ row.confidence }}</span>
                </div>
              </td>
              <td style="font-size: 13px; color: #a0a0a0">{{ formatRelative(row.started_at) }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Mobile card layout (< 640px) -->
      <div class="mobile-cards">
        <div
          v-for="row in activeRows"
          :key="'m-' + row.incident_id"
          class="mobile-attack-card"
          :style="rowStyle(row)"
        >
          <div class="mobile-line1">
            <span class="mono">{{ row.host_ip }}</span>
            <NTag :type="row.vector === 'udp_flood' ? 'error' : 'warning'" size="small" round>
              {{ vectorLabel(row.vector) }}
            </NTag>
          </div>
          <div class="mobile-line2">
            <span class="mono">{{ formatPps(row.pps) }} pps</span>
            <span class="mono">{{ formatBps(row.bps) }}</span>
          </div>
          <div class="mobile-line3">
            <span>{{ formatDuration(row.started_at) }}</span>
            <span>Score: {{ row.confidence }}</span>
          </div>
        </div>
      </div>
    </template>
  </AppLayout>
</template>

<style scoped>
.page-title {
  font-size: 20px;
  font-weight: 600;
  color: #e0e0e0;
  margin: 0;
}

.summary-strip {
  display: flex;
  gap: 16px;
  margin-bottom: 24px;
}

.summary-card {
  flex: 1;
  background: #1c1c21;
  padding: 16px;
  border-radius: 8px;
}

.summary-value {
  font-size: 24px;
  font-weight: 600;
  color: #e0e0e0;
  font-family: ui-monospace, monospace;
}

.summary-label {
  font-size: 12px;
  color: #a0a0a0;
  margin-top: 4px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
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
  max-width: 360px;
  text-align: center;
  display: block;
  margin-top: 8px;
}

/* Desktop table */
.attack-table-wrapper {
  overflow-x: auto;
  border-radius: 8px;
  background: #1c1c21;
}

.attack-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 14px;
}

.attack-table th {
  padding: 10px 14px;
  font-size: 12px;
  font-weight: 600;
  color: #a0a0a0;
  text-align: left;
  border-bottom: 1px solid #2a2a30;
  white-space: nowrap;
}

.attack-table td {
  padding: 10px 14px;
  border-bottom: 1px solid #1e1e24;
  color: #e0e0e0;
  vertical-align: middle;
}

.attack-row {
  transition: background-color 1s, opacity 0.5s;
}

.attack-row:hover td {
  background-color: rgba(255, 255, 255, 0.03);
}

.mono {
  font-family: 'JetBrains Mono', 'Fira Code', ui-monospace, monospace;
}

.right {
  text-align: right;
}

/* Mobile cards — hidden on desktop */
.mobile-cards {
  display: none;
}

@media (max-width: 639px) {
  .attack-table-wrapper {
    display: none;
  }

  .mobile-cards {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .mobile-attack-card {
    background: #1c1c21;
    border-radius: 8px;
    padding: 12px 16px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    border-left: 3px solid transparent;
  }

  .mobile-line1 {
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-size: 14px;
    font-weight: 600;
    color: #e0e0e0;
  }

  .mobile-line2 {
    display: flex;
    gap: 16px;
    font-size: 13px;
    color: #c0c0c0;
  }

  .mobile-line3 {
    display: flex;
    gap: 16px;
    font-size: 12px;
    color: #a0a0a0;
  }

  /* hide summary grid on mobile — stack */
  .summary-strip {
    flex-direction: column;
    gap: 8px;
  }
}
</style>
