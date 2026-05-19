<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { useRoute } from 'vue-router';
import { useI18n } from 'vue-i18n';
import {
  NSpin,
  NAlert,
  NButton,
  NTag,
  NTimeline,
  NTimelineItem,
  NBreadcrumb,
  NBreadcrumbItem,
  NResult,
} from 'naive-ui';
import AppLayout from '@/components/AppLayout.vue';
import { api, type ApiError } from '@/api/client';

const { t } = useI18n();
const route = useRoute();

interface IncidentUpdate {
  id: string;
  incident_id: string;
  state: string;
  pps: number;
  bps: number;
  confidence: number;
  recorded_at: string;
}

interface IncidentDetail {
  id: string;
  host_ip: string;
  vector: string;
  hostgroup: string;
  peak_pps: number;
  peak_bps: number;
  score: number;
  started_at: string;
  ended_at: string | null;
  updates: IncidentUpdate[];
}

const incident = ref<IncidentDetail | null>(null);
const loading = ref(true);
const notFound = ref(false);
const error = ref<string | null>(null);

const incidentId = route.params.id as string;

async function loadIncident() {
  loading.value = true;
  notFound.value = false;
  error.value = null;
  try {
    incident.value = await api<IncidentDetail>(`/api/incidents/${incidentId}`);
  } catch (err) {
    const apiErr = err as ApiError;
    if (apiErr.status === 404) {
      notFound.value = true;
    } else {
      error.value = 'error';
    }
  } finally {
    loading.value = false;
  }
}

onMounted(loadIncident);

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

function formatDuration(startedAt: string, endedAt: string | null): string {
  const end = endedAt ? new Date(endedAt) : new Date();
  const secs = Math.floor((end.getTime() - new Date(startedAt).getTime()) / 1000);
  if (secs < 60) return secs + 's';
  if (secs < 3600) return Math.floor(secs / 60) + 'm ' + (secs % 60) + 's';
  return Math.floor(secs / 3600) + 'h ' + Math.floor((secs % 3600) / 60) + 'm';
}

function formatTimestamp(ts: string): string {
  return new Date(ts).toLocaleString('pt-BR');
}

function vectorLabel(vector: string): string {
  if (vector === 'udp_flood') return t('vetor.udp_flood');
  if (vector === 'icmp_flood') return t('vetor.icmp_flood');
  return vector;
}

function timelineType(state: string): 'success' | 'warning' | 'error' | 'default' {
  if (state === 'started') return 'error';
  if (state === 'ended') return 'success';
  return 'warning';
}
</script>

<template>
  <AppLayout>
    <template #header-title>
      <NBreadcrumb>
        <NBreadcrumbItem href="/incidents">{{ t('incidentes.breadcrumb') }}</NBreadcrumbItem>
        <NBreadcrumbItem>{{ incidentId.slice(0, 12) }}</NBreadcrumbItem>
      </NBreadcrumb>
    </template>

    <!-- Loading -->
    <div v-if="loading" class="state-center">
      <NSpin size="large" />
    </div>

    <!-- 404 -->
    <NResult
      v-else-if="notFound"
      status="404"
      :title="t('incidentes.nao_encontrado')"
    />

    <!-- Error -->
    <NAlert v-else-if="error" type="error" style="margin-bottom: 16px">
      <template #default>
        <NButton text @click="loadIncident">{{ t('actions.retry') }}</NButton>
      </template>
    </NAlert>

    <!-- Detail -->
    <template v-else-if="incident">
      <!-- Header -->
      <div class="detail-header">
        <span class="host-ip mono">{{ incident.host_ip }}</span>
        <div class="header-badges">
          <NTag
            :type="incident.vector === 'udp_flood' ? 'error' : 'warning'"
            size="medium"
            round
          >
            {{ vectorLabel(incident.vector) }}
          </NTag>
          <NTag
            :type="incident.ended_at ? 'default' : 'success'"
            size="medium"
            round
          >
            {{ incident.ended_at ? t('incidentes.status.encerrado') : t('incidentes.status.ativo') }}
          </NTag>
        </div>
      </div>

      <!-- Metrics row -->
      <div class="metrics-row">
        <div class="metric-card">
          <div class="metric-value mono">{{ formatPps(incident.peak_pps) }}</div>
          <div class="metric-label">{{ t('detail.pps_pico') }}</div>
        </div>
        <div class="metric-card">
          <div class="metric-value mono">{{ formatBps(incident.peak_bps) }}</div>
          <div class="metric-label">{{ t('detail.bps_pico') }}</div>
        </div>
        <div class="metric-card">
          <div class="metric-value">{{ incident.score }}</div>
          <div class="metric-label">{{ t('detail.score') }}</div>
        </div>
        <div class="metric-card">
          <div class="metric-value">{{ formatDuration(incident.started_at, incident.ended_at) }}</div>
          <div class="metric-label">{{ t('detail.duracao') }}</div>
        </div>
      </div>

      <!-- Timeline -->
      <div class="section">
        <h2 class="section-title">{{ t('detail.timeline') }}</h2>
        <NTimeline>
          <NTimelineItem
            type="error"
            :title="'Ataque iniciado'"
            :time="formatTimestamp(incident.started_at)"
            :content="`${formatPps(incident.peak_pps)} · ${formatBps(incident.peak_bps)} · Score ${incident.score}`"
          />
          <NTimelineItem
            v-for="upd in (incident.updates ?? [])"
            :key="upd.id"
            :type="timelineType(upd.state)"
            :title="upd.state === 'update' ? 'Atualização' : upd.state"
            :time="formatTimestamp(upd.recorded_at)"
            :content="`${formatPps(upd.pps)} · ${formatBps(upd.bps)} · Score ${upd.confidence}`"
          />
          <NTimelineItem
            v-if="incident.ended_at"
            type="success"
            :title="'Ataque encerrado'"
            :time="formatTimestamp(incident.ended_at)"
          />
        </NTimeline>
      </div>

      <!-- Raw detail -->
      <div class="section">
        <h2 class="section-title">{{ t('detail.vetor') }}</h2>
        <div class="raw-grid">
          <div class="raw-item">
            <span class="raw-label">{{ t('detail.vetor') }}</span>
            <span class="raw-value">{{ vectorLabel(incident.vector) }}</span>
          </div>
          <div class="raw-item">
            <span class="raw-label">Hostgroup</span>
            <span class="raw-value mono">{{ incident.hostgroup || '—' }}</span>
          </div>
          <div class="raw-item">
            <span class="raw-label">{{ t('detail.criterios') }}</span>
            <span class="raw-value">
              PPS: {{ formatPps(incident.peak_pps) }} ·
              BPS: {{ formatBps(incident.peak_bps) }}
            </span>
          </div>
        </div>
      </div>
    </template>
  </AppLayout>
</template>

<style scoped>
.state-center {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 200px;
}

.detail-header {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 24px;
  flex-wrap: wrap;
}

.host-ip {
  font-size: 28px;
  font-weight: 600;
  color: #e0e0e0;
  font-family: 'JetBrains Mono', 'Fira Code', ui-monospace, monospace;
}

.header-badges {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.metrics-row {
  display: flex;
  gap: 16px;
  margin-bottom: 32px;
  flex-wrap: wrap;
}

.metric-card {
  flex: 1;
  min-width: 120px;
  background: #1c1c21;
  border-radius: 8px;
  padding: 16px;
}

.metric-value {
  font-size: 20px;
  font-weight: 600;
  color: #e0e0e0;
  margin-bottom: 4px;
}

.mono {
  font-family: 'JetBrains Mono', 'Fira Code', ui-monospace, monospace;
}

.metric-label {
  font-size: 12px;
  color: #a0a0a0;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.section {
  margin-bottom: 32px;
}

.section-title {
  font-size: 16px;
  font-weight: 600;
  color: #c0c0c0;
  margin: 0 0 16px;
  padding-bottom: 8px;
  border-bottom: 1px solid #2a2a30;
}

.raw-grid {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.raw-item {
  display: flex;
  gap: 16px;
  font-size: 14px;
}

.raw-label {
  color: #a0a0a0;
  width: 160px;
  flex-shrink: 0;
}

.raw-value {
  color: #e0e0e0;
}
</style>
