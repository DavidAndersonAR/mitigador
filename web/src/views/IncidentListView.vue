<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue';
import { useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import { NSpin, NEmpty, NAlert, NButton, NTag, NPagination } from 'naive-ui';
import AppLayout from '@/components/AppLayout.vue';
import { api } from '@/api/client';

const { t } = useI18n();
const router = useRouter();

interface Incident {
  id: string;
  host_ip: string;
  vector: string;
  peak_pps: number;
  peak_bps: number;
  score: number;
  started_at: string;
  ended_at: string | null;
}

const incidents = ref<Incident[]>([]);
const total = ref(0);
const loading = ref(true);
const error = ref<string | null>(null);
const page = ref(1);
const pageSize = 50;

const offset = computed(() => (page.value - 1) * pageSize);

async function loadIncidents() {
  loading.value = true;
  error.value = null;
  try {
    const res = await api<{ items: Incident[]; total: number }>(
      `/api/incidents?limit=${pageSize}&offset=${offset.value}`
    );
    incidents.value = res.items;
    total.value = res.total;
  } catch {
    error.value = 'error';
  } finally {
    loading.value = false;
  }
}

async function retry() {
  await loadIncidents();
}

watch(page, loadIncidents);

onMounted(loadIncidents);

function truncateId(id: string): string {
  return id.slice(0, 12);
}

function formatBps(bps: number): string {
  if (bps >= 1e9) return (bps / 1e9).toFixed(1) + ' Gbps';
  if (bps >= 1e6) return (bps / 1e6).toFixed(1) + ' Mbps';
  if (bps >= 1e3) return (bps / 1e3).toFixed(1) + ' Kbps';
  return bps + ' bps';
}

function formatPps(pps: number): string {
  if (pps >= 1e6) return (pps / 1e6).toFixed(1) + 'M pps';
  if (pps >= 1e3) return (pps / 1e3).toFixed(1) + 'K pps';
  return pps + ' pps';
}

function formatTimestamp(ts: string): string {
  const d = new Date(ts);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${pad(d.getDate())}/${pad(d.getMonth() + 1)}/${d.getFullYear()} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function formatDuration(startedAt: string, endedAt: string | null): string {
  const end = endedAt ? new Date(endedAt) : new Date();
  const secs = Math.floor((end.getTime() - new Date(startedAt).getTime()) / 1000);
  if (secs < 60) return secs + 's';
  if (secs < 3600) return Math.floor(secs / 60) + 'm ' + (secs % 60) + 's';
  return Math.floor(secs / 3600) + 'h ' + Math.floor((secs % 3600) / 60) + 'm';
}

function vectorLabel(vector: string): string {
  if (vector === 'udp_flood') return t('vetor.udp_flood');
  if (vector === 'icmp_flood') return t('vetor.icmp_flood');
  return vector;
}

function goToIncident(id: string) {
  router.push('/incidents/' + id);
}
</script>

<template>
  <AppLayout>
    <template #header-title>
      <h1 class="page-title">{{ t('incidentes.titulo') }}</h1>
    </template>

    <!-- Loading -->
    <div v-if="loading" class="state-center">
      <NSpin size="large" />
    </div>

    <!-- Error -->
    <NAlert v-else-if="error" type="error" style="margin-bottom: 16px">
      <template #default>
        <NButton text @click="retry">{{ t('actions.retry') }}</NButton>
      </template>
    </NAlert>

    <!-- Empty -->
    <div v-else-if="incidents.length === 0" class="state-center">
      <NEmpty :description="t('incidentes.vazio.titulo')">
        <template #extra>
          <span class="empty-body">{{ t('incidentes.vazio.body') }}</span>
        </template>
      </NEmpty>
    </div>

    <!-- Table -->
    <template v-else>
      <div class="table-wrapper">
        <table class="data-table">
          <thead>
            <tr>
              <th>{{ t('incidentes.col.id') }}</th>
              <th>{{ t('incidentes.col.ip') }}</th>
              <th>{{ t('incidentes.col.vetor') }}</th>
              <th class="right">{{ t('incidentes.col.pps_pico') }}</th>
              <th class="right">{{ t('incidentes.col.bps_pico') }}</th>
              <th class="right">{{ t('incidentes.col.score') }}</th>
              <th>{{ t('incidentes.col.inicio') }}</th>
              <th>{{ t('incidentes.col.duracao') }}</th>
              <th>{{ t('incidentes.col.status') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="inc in incidents"
              :key="inc.id"
              class="clickable-row"
              @click="goToIncident(inc.id)"
            >
              <td class="mono" style="color: #a0a0a0; font-size: 12px">{{ truncateId(inc.id) }}</td>
              <td class="mono">{{ inc.host_ip }}</td>
              <td>
                <NTag
                  :type="inc.vector === 'udp_flood' ? 'error' : 'warning'"
                  size="small"
                  round
                >
                  {{ vectorLabel(inc.vector) }}
                </NTag>
              </td>
              <td class="mono right">{{ formatPps(inc.peak_pps) }}</td>
              <td class="mono right">{{ formatBps(inc.peak_bps) }}</td>
              <td class="right">{{ inc.score }}</td>
              <td style="font-size: 13px; color: #c0c0c0; white-space: nowrap">
                {{ formatTimestamp(inc.started_at) }}
              </td>
              <td>{{ formatDuration(inc.started_at, inc.ended_at) }}</td>
              <td>
                <NTag
                  :type="inc.ended_at ? 'default' : 'success'"
                  size="small"
                  round
                >
                  {{ inc.ended_at ? t('incidentes.status.encerrado') : t('incidentes.status.ativo') }}
                </NTag>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="pagination-wrapper">
        <NPagination
          v-model:page="page"
          :page-count="Math.ceil(total / pageSize)"
          :page-slot="5"
          show-quick-jumper
        />
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

.clickable-row {
  cursor: pointer;
}

.clickable-row:hover td {
  background-color: rgba(255, 255, 255, 0.04);
}

.mono {
  font-family: 'JetBrains Mono', 'Fira Code', ui-monospace, monospace;
}

.right {
  text-align: right;
}

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
