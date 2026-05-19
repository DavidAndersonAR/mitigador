<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue';
import { useI18n } from 'vue-i18n';
import { NCard, NEmpty, NSpin, NAlert, NTag, NRadioGroup, NRadio } from 'naive-ui';
import AppLayout from '@/components/AppLayout.vue';
import HostTrafficChart from '@/components/HostTrafficChart.vue';
import {
  fetchOverview,
  fetchRecent,
  type DashboardOverview,
  type DashboardRecent,
  type DashboardProto,
  type DashboardDominantProto,
} from '@/api/dashboard';
import type { HostBucket } from '@/api/traffic';

const { t } = useI18n();

const overview = ref<DashboardOverview | null>(null);
const recent = ref<DashboardRecent | null>(null);
const loadingFirst = ref(true);
const overviewError = ref(false);
const recentError = ref(false);
const mode = ref<'bps' | 'pps'>('bps');

let overviewInterval: ReturnType<typeof setInterval> | null = null;
let recentInterval: ReturnType<typeof setInterval> | null = null;

// ── formatters ──────────────────────────────────────────────────────────
function formatBps(bps: number): string {
  if (bps >= 1e9) return (bps / 1e9).toFixed(2) + ' Gbps';
  if (bps >= 1e6) return (bps / 1e6).toFixed(2) + ' Mbps';
  if (bps >= 1e3) return (bps / 1e3).toFixed(1) + ' Kbps';
  return bps + ' bps';
}
function formatPps(pps: number): string {
  if (pps >= 1e6) return (pps / 1e6).toFixed(2) + ' Mpps';
  if (pps >= 1e3) return (pps / 1e3).toFixed(1) + ' Kpps';
  return pps + ' pps';
}
function formatBytes(bytes: number): string {
  if (bytes >= 1e9) return (bytes / 1e9).toFixed(2) + ' GB';
  if (bytes >= 1e6) return (bytes / 1e6).toFixed(1) + ' MB';
  if (bytes >= 1e3) return (bytes / 1e3).toFixed(1) + ' KB';
  return bytes + ' B';
}
function formatRelative(ms: number): string {
  const delta = Math.max(0, Math.floor((Date.now() - ms) / 1000));
  if (delta < 1) return t('flows.now');
  if (delta < 60) return `${delta}s`;
  return `${Math.floor(delta / 60)}m`;
}
function protoTagType(proto: DashboardDominantProto | DashboardProto): 'error' | 'warning' | 'success' | 'info' {
  if (proto === 'udp') return 'error';
  if (proto === 'icmp') return 'warning';
  if (proto === 'tcp') return 'info';
  return 'success';
}
function hostgroupLabel(hg: string | null): string {
  return hg ?? t('flows.hostgroup.none');
}

// ── proto-share computed ────────────────────────────────────────────────
const protoShare = computed(() => {
  const ov = overview.value;
  if (!ov) return { udp: 0, icmp: 0, other: 0, total: 0 };
  const src = mode.value === 'bps' ? ov.proto_bps : ov.proto_pps;
  const total = src.udp + src.icmp + src.other;
  return { ...src, total };
});

function pct(part: number, total: number): number {
  if (total <= 0) return 0;
  return Math.round((part * 1000) / total) / 10;
}

// ── timeseries: feed HostTrafficChart with the global buckets ───────────
const globalBuckets = computed<HostBucket[]>(() => {
  const ov = overview.value;
  if (!ov) return [];
  return ov.buckets.map((b, i) => ({
    offset_s: i,
    pps: b.pps,
    bps: b.bps,
    pps_udp: b.pps_udp,
    bps_udp: b.bps_udp,
    pps_icmp: b.pps_icmp,
    bps_icmp: b.bps_icmp,
    pps_other: b.pps_other,
    bps_other: b.bps_other,
  }));
});

// ── sparkline (SVG polyline) ────────────────────────────────────────────
function sparkPath(values: number[]): string {
  if (!values.length) return '';
  // values are newest-first; reverse so time flows left→right.
  const data = [...values].reverse();
  const w = 120;
  const h = 28;
  const max = Math.max(1, ...data);
  const step = w / Math.max(1, data.length - 1);
  return data
    .map((v, i) => {
      const x = i * step;
      const y = h - (v / max) * (h - 2) - 1;
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(' ');
}

function topBpsMax(): number {
  const ov = overview.value;
  if (!ov || ov.top.length === 0) return 0;
  return Math.max(...ov.top.map((r) => r.bps));
}

// ── data fetching ───────────────────────────────────────────────────────
async function loadOverview() {
  try {
    overview.value = await fetchOverview();
    overviewError.value = false;
  } catch {
    overviewError.value = true;
  } finally {
    loadingFirst.value = false;
  }
}

async function loadRecent() {
  try {
    recent.value = await fetchRecent(50);
    recentError.value = false;
  } catch {
    recentError.value = true;
  }
}

function startPolling() {
  if (overviewInterval === null) {
    overviewInterval = setInterval(() => { void loadOverview(); }, 1000);
  }
  if (recentInterval === null) {
    recentInterval = setInterval(() => { void loadRecent(); }, 1000);
  }
}
function stopPolling() {
  if (overviewInterval !== null) { clearInterval(overviewInterval); overviewInterval = null; }
  if (recentInterval !== null) { clearInterval(recentInterval); recentInterval = null; }
}
function onVisibilityChange() {
  if (document.visibilityState === 'visible') {
    void loadOverview();
    void loadRecent();
    startPolling();
  } else {
    stopPolling();
  }
}

onMounted(() => {
  void loadOverview();
  void loadRecent();
  startPolling();
  document.addEventListener('visibilitychange', onVisibilityChange);
});
onBeforeUnmount(() => {
  stopPolling();
  document.removeEventListener('visibilitychange', onVisibilityChange);
});
</script>

<template>
  <AppLayout>
    <div class="flows-page">
      <header class="flows-header">
        <h1>{{ t('flows.title') }}</h1>
        <NRadioGroup v-model:value="mode" size="small">
          <NRadio value="bps">{{ t('flows.mode.bps') }}</NRadio>
          <NRadio value="pps">{{ t('flows.mode.pps') }}</NRadio>
        </NRadioGroup>
      </header>

      <NAlert v-if="overviewError && !loadingFirst" type="warning" :show-icon="false" class="alert">
        {{ t('flows.error.overview') }}
      </NAlert>

      <!-- KPI cards ─────────────────────────────────────────── -->
      <section class="kpi-grid">
        <NCard size="small" class="kpi-card kpi-bps">
          <div class="kpi-label">{{ t('flows.kpi.bps_now') }}</div>
          <div class="kpi-value">{{ overview ? formatBps(overview.kpis.bps_now) : '—' }}</div>
          <div class="kpi-sub">{{ t('flows.kpi.bps_avg') }}: {{ overview ? formatBps(overview.kpis.bps_avg) : '—' }}</div>
        </NCard>
        <NCard size="small" class="kpi-card kpi-pps">
          <div class="kpi-label">{{ t('flows.kpi.pps_now') }}</div>
          <div class="kpi-value">{{ overview ? formatPps(overview.kpis.pps_now) : '—' }}</div>
          <div class="kpi-sub">{{ t('flows.kpi.pps_avg') }}: {{ overview ? formatPps(overview.kpis.pps_avg) : '—' }}</div>
        </NCard>
        <NCard size="small" class="kpi-card kpi-hosts">
          <div class="kpi-label">{{ t('flows.kpi.active_hosts') }}</div>
          <div class="kpi-value">{{ overview?.kpis.active_hosts ?? '—' }}</div>
          <div class="kpi-sub">{{ t('flows.kpi.window_60s') }}</div>
        </NCard>
        <NCard size="small" class="kpi-card kpi-exporters">
          <div class="kpi-label">{{ t('flows.kpi.exporters') }}</div>
          <div class="kpi-value">
            <span class="kpi-online">{{ overview?.kpis.exporters_online ?? '—' }}</span>
            <span class="kpi-divider">/</span>
            <span class="kpi-total">{{ overview?.kpis.exporters_total ?? '—' }}</span>
          </div>
          <div class="kpi-sub">{{ t('flows.kpi.exporters_sub') }}</div>
        </NCard>
      </section>

      <!-- Main timeseries chart ─────────────────────────────── -->
      <NCard size="small" class="chart-card" :title="t('flows.chart.title')">
        <div v-if="loadingFirst" class="chart-loading"><NSpin /></div>
        <HostTrafficChart
          v-else-if="globalBuckets.length > 0"
          :buckets="globalBuckets"
          :mode="mode"
          :height="280"
        />
        <NEmpty v-else :description="t('flows.chart.empty')" />
      </NCard>

      <!-- Top-10 + proto share row ──────────────────────────── -->
      <section class="row-2col">
        <NCard size="small" :title="t('flows.top.title')" class="top-card">
          <div v-if="loadingFirst" class="chart-loading"><NSpin /></div>
          <NEmpty v-else-if="!overview || overview.top.length === 0" :description="t('flows.top.empty')" />
          <ul v-else class="top-list">
            <li v-for="row in overview.top" :key="row.ip" class="top-row">
              <div class="top-ip">
                <NTag :type="protoTagType(row.dominant_proto)" size="small" :bordered="false">
                  {{ t('flows.proto.' + row.dominant_proto) }}
                </NTag>
                <div class="top-ip-stack">
                  <span v-if="row.owner" class="owner-chip" :title="row.owner">{{ row.owner }}</span>
                  <span class="ip">{{ row.ip }}</span>
                  <span class="hostname" :title="row.hostname || ''">{{ row.hostname || hostgroupLabel(row.hostgroup) }}</span>
                </div>
              </div>
              <div class="top-bar-wrap">
                <div class="top-bar" :style="{ width: topBpsMax() ? (row.bps / topBpsMax() * 100) + '%' : '0%' }"></div>
                <svg class="spark" viewBox="0 0 120 28" preserveAspectRatio="none">
                  <path :d="sparkPath(row.sparkline)" />
                </svg>
              </div>
              <div class="top-metric">
                <div class="top-bps">{{ formatBps(row.bps) }}</div>
                <div class="top-pps">{{ formatPps(row.pps) }}</div>
              </div>
            </li>
          </ul>
        </NCard>

        <NCard size="small" :title="t('flows.proto.title')" class="proto-card">
          <NEmpty v-if="protoShare.total === 0" :description="t('flows.proto.empty')" />
          <ul v-else class="proto-list">
            <li class="proto-row">
              <div class="proto-label"><span class="dot dot-udp"></span>UDP</div>
              <div class="proto-bar-wrap"><div class="proto-bar proto-udp" :style="{ width: pct(protoShare.udp, protoShare.total) + '%' }"></div></div>
              <div class="proto-pct">{{ pct(protoShare.udp, protoShare.total) }}%</div>
            </li>
            <li class="proto-row">
              <div class="proto-label"><span class="dot dot-icmp"></span>ICMP</div>
              <div class="proto-bar-wrap"><div class="proto-bar proto-icmp" :style="{ width: pct(protoShare.icmp, protoShare.total) + '%' }"></div></div>
              <div class="proto-pct">{{ pct(protoShare.icmp, protoShare.total) }}%</div>
            </li>
            <li class="proto-row">
              <div class="proto-label"><span class="dot dot-other"></span>{{ t('flows.proto.other') }}</div>
              <div class="proto-bar-wrap"><div class="proto-bar proto-other" :style="{ width: pct(protoShare.other, protoShare.total) + '%' }"></div></div>
              <div class="proto-pct">{{ pct(protoShare.other, protoShare.total) }}%</div>
            </li>
          </ul>
          <div class="proto-foot">
            {{ t('flows.proto.foot', { mode: mode.toUpperCase() }) }}
          </div>
        </NCard>
      </section>

      <!-- Recent flows table ────────────────────────────────── -->
      <NCard size="small" :title="t('flows.recent.title')" class="recent-card">
        <NAlert v-if="recentError" type="warning" :show-icon="false" class="alert">
          {{ t('flows.error.recent') }}
        </NAlert>
        <NEmpty v-else-if="!recent || recent.flows.length === 0" :description="t('flows.recent.empty')" />
        <table v-else class="recent-table">
          <thead>
            <tr>
              <th>{{ t('flows.recent.col.when') }}</th>
              <th>{{ t('flows.recent.col.src') }}</th>
              <th>{{ t('flows.recent.col.arrow') }}</th>
              <th>{{ t('flows.recent.col.dst') }}</th>
              <th>{{ t('flows.recent.col.proto') }}</th>
              <th>{{ t('flows.recent.col.bytes') }}</th>
              <th>{{ t('flows.recent.col.pkts') }}</th>
              <th>{{ t('flows.recent.col.avg_pkt') }}</th>
              <th>{{ t('flows.recent.col.sample_rate') }}</th>
              <th>{{ t('flows.recent.col.exporter') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(f, idx) in recent.flows" :key="idx + '-' + f.received_ms">
              <td class="when">{{ formatRelative(f.received_ms) }}</td>
              <td class="addr">
                <span v-if="f.src_owner" class="owner-chip-sm" :title="f.src_owner">{{ f.src_owner }}</span>
                <span class="ip">{{ f.src_ip }}</span>
                <span v-if="f.src_hostname" class="hostname" :title="f.src_hostname">{{ f.src_hostname }}</span>
              </td>
              <td class="arrow">→</td>
              <td class="addr">
                <span v-if="f.dst_owner" class="owner-chip-sm" :title="f.dst_owner">{{ f.dst_owner }}</span>
                <span class="ip">{{ f.dst_ip }}</span>
                <span v-if="f.dst_hostname" class="hostname" :title="f.dst_hostname">{{ f.dst_hostname }}</span>
                <span v-else-if="f.dst_hostgroup" class="hg-inline">{{ f.dst_hostgroup }}</span>
              </td>
              <td>
                <NTag :type="protoTagType(f.proto)" size="small" :bordered="false">
                  {{ f.proto.toUpperCase() }}
                </NTag>
              </td>
              <td class="num">{{ formatBytes(f.bytes) }}</td>
              <td class="num">{{ f.packets }}</td>
              <td class="num">{{ f.avg_pkt_bytes ? f.avg_pkt_bytes + ' B' : '—' }}</td>
              <td class="num">{{ f.sample_rate || '—' }}</td>
              <td class="exp">{{ f.exporter }}</td>
            </tr>
          </tbody>
        </table>
      </NCard>
    </div>
  </AppLayout>
</template>

<style scoped>
.flows-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 16px 24px 24px;
  min-height: 100%;
}
.flows-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.flows-header h1 {
  font-size: 22px;
  font-weight: 600;
  color: #e0e0e0;
  margin: 0;
}
.alert { margin: 0; }

/* KPIs */
.kpi-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.kpi-card { border-left: 3px solid #2a2a30; }
.kpi-bps { border-left-color: #18a058; }
.kpi-pps { border-left-color: #2080f0; }
.kpi-hosts { border-left-color: #f0a020; }
.kpi-exporters { border-left-color: #8a2be2; }
.kpi-label {
  font-size: 11px;
  color: #888;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 4px;
}
.kpi-value {
  font-size: 22px;
  font-weight: 600;
  color: #e6e6e6;
  font-variant-numeric: tabular-nums;
}
.kpi-online { color: #18a058; }
.kpi-divider { color: #555; margin: 0 4px; }
.kpi-total { color: #aaa; }
.kpi-sub { font-size: 11px; color: #888; margin-top: 4px; }

/* Main chart */
.chart-card { min-height: 280px; }
.chart-loading {
  height: 280px;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* 2-column row */
.row-2col {
  display: grid;
  grid-template-columns: 2fr 1fr;
  gap: 12px;
}

/* Top list */
.top-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.top-row {
  display: grid;
  grid-template-columns: 240px 1fr 130px;
  gap: 12px;
  align-items: center;
  padding: 6px 0;
  border-bottom: 1px solid #232328;
}
.top-row:last-child { border-bottom: none; }
.top-ip {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.top-ip-stack {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  min-width: 0;
  max-width: 100%;
  line-height: 1.2;
  gap: 2px;
}
.top-ip-stack .ip {
  font-family: 'JetBrains Mono', 'Fira Code', ui-monospace, monospace;
  color: #e0e0e0;
  font-size: 13px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 100%;
}
.top-ip-stack .hostname {
  color: #888;
  font-size: 11px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 100%;
}
.owner-chip {
  display: inline-block;
  background: linear-gradient(90deg, #2080f0 0%, #08a0e0 100%);
  color: #fff;
  font-size: 10px;
  font-weight: 600;
  padding: 1px 8px;
  border-radius: 10px;
  letter-spacing: 0.2px;
  max-width: 100%;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.owner-chip-sm {
  display: inline-block;
  background: rgba(32, 128, 240, 0.18);
  color: #62a6ff;
  font-size: 10px;
  font-weight: 600;
  padding: 1px 6px;
  border-radius: 8px;
  letter-spacing: 0.2px;
  max-width: 100%;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.top-bar-wrap {
  position: relative;
  height: 28px;
  background: #1a1a1f;
  border-radius: 4px;
  overflow: hidden;
}
.top-bar {
  position: absolute;
  inset: 0 auto 0 0;
  background: linear-gradient(90deg, rgba(208, 48, 80, 0.35), rgba(240, 160, 32, 0.35));
  transition: width 0.35s ease;
}
.spark {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  pointer-events: none;
}
.spark path {
  fill: none;
  stroke: #18a058;
  stroke-width: 1.5;
  vector-effect: non-scaling-stroke;
}
.top-metric {
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.top-bps { color: #e0e0e0; font-size: 13px; font-weight: 600; }
.top-pps { color: #888; font-size: 11px; margin-top: 2px; }

/* Proto card */
.proto-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.proto-row {
  display: grid;
  grid-template-columns: 80px 1fr 56px;
  gap: 12px;
  align-items: center;
  font-size: 13px;
}
.proto-label { color: #c0c0c0; display: flex; align-items: center; gap: 8px; }
.dot { display: inline-block; width: 10px; height: 10px; border-radius: 50%; }
.dot-udp   { background: #d03050; }
.dot-icmp  { background: #f0a020; }
.dot-other { background: #18a058; }
.proto-bar-wrap {
  position: relative;
  height: 12px;
  background: #1a1a1f;
  border-radius: 3px;
  overflow: hidden;
}
.proto-bar {
  position: absolute;
  inset: 0 auto 0 0;
  height: 100%;
  transition: width 0.35s ease;
}
.proto-udp   { background: #d03050; }
.proto-icmp  { background: #f0a020; }
.proto-other { background: #18a058; }
.proto-pct {
  text-align: right;
  color: #e0e0e0;
  font-variant-numeric: tabular-nums;
}
.proto-foot {
  margin-top: 14px;
  font-size: 11px;
  color: #777;
}

/* Recent flows table */
.recent-card { min-height: 240px; }
.recent-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}
.recent-table th {
  text-align: left;
  font-weight: 500;
  color: #888;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  padding: 8px 8px;
  border-bottom: 1px solid #2a2a30;
}
.recent-table td {
  padding: 6px 8px;
  border-bottom: 1px solid #1f1f24;
  color: #d8d8d8;
  font-variant-numeric: tabular-nums;
}
.recent-table tr:last-child td { border-bottom: none; }
.recent-table .when { color: #888; width: 56px; }
.recent-table .ip { font-family: 'JetBrains Mono', 'Fira Code', ui-monospace, monospace; }
.recent-table .addr {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  line-height: 1.2;
  min-width: 0;
  max-width: 240px;
  gap: 2px;
}
.recent-table .addr .ip {
  color: #e0e0e0;
  font-size: 13px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 100%;
}
.recent-table .addr .hostname {
  color: #888;
  font-size: 11px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 100%;
}
.recent-table .addr .hg-inline {
  color: #f0a020;
  font-size: 11px;
}
.recent-table .arrow { width: 22px; color: #555; text-align: center; }
.recent-table .num { text-align: right; }
.recent-table .exp { color: #888; font-family: 'JetBrains Mono', 'Fira Code', ui-monospace, monospace; }

/* Mobile */
@media (max-width: 880px) {
  .flows-page { padding: 12px; }
  .kpi-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .row-2col { grid-template-columns: 1fr; }
  .top-row { grid-template-columns: 1fr; gap: 4px; }
  .top-metric { text-align: left; display: flex; gap: 12px; }
  .recent-table .exp { display: none; }
  .recent-table th:last-child { display: none; }
}
</style>
