<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue';
import { useI18n } from 'vue-i18n';
import { NCard, NEmpty, NSpin, NAlert, NRadioGroup, NRadio } from 'naive-ui';
import AppLayout from '@/components/AppLayout.vue';

// ECharts — tree-shaken: only Sankey + Pie + Bar.
import { use } from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { SankeyChart, PieChart, BarChart } from 'echarts/charts';
import {
  TooltipComponent,
  TitleComponent,
  LegendComponent,
  GridComponent,
} from 'echarts/components';
import VChart from 'vue-echarts';

use([
  CanvasRenderer,
  SankeyChart,
  PieChart,
  BarChart,
  TooltipComponent,
  TitleComponent,
  LegendComponent,
  GridComponent,
]);

import {
  fetchAnalytics,
  fetchOverview,
  type DashboardAnalytics,
  type DashboardOverview,
} from '@/api/dashboard';

const { t } = useI18n();

const overview = ref<DashboardOverview | null>(null);
const analytics = ref<DashboardAnalytics | null>(null);
const loadingFirst = ref(true);
const errored = ref(false);
const mode = ref<'bps' | 'pps'>('bps');

let interval: ReturnType<typeof setInterval> | null = null;

// ─── formatters ─────────────────────────────────────────────────────────
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
function formatMetric(v: number) {
  return mode.value === 'bps' ? formatBps(v) : formatPps(v);
}
function flagEmoji(iso: string): string {
  if (!iso || iso === '??' || iso.length !== 2) return '🏳';
  const A = 0x1f1e6;
  return String.fromCodePoint(A + iso.charCodeAt(0) - 65, A + iso.charCodeAt(1) - 65);
}

// ─── data loading ───────────────────────────────────────────────────────
async function loadAll() {
  try {
    const [a, o] = await Promise.all([fetchAnalytics(), fetchOverview()]);
    analytics.value = a;
    overview.value = o;
    errored.value = false;
  } catch {
    errored.value = true;
  } finally {
    loadingFirst.value = false;
  }
}

function startPolling() {
  if (interval !== null) return;
  interval = setInterval(() => { void loadAll(); }, 2000);
}
function stopPolling() {
  if (interval !== null) { clearInterval(interval); interval = null; }
}
function onVis() {
  if (document.visibilityState === 'visible') { void loadAll(); startPolling(); }
  else stopPolling();
}

onMounted(() => { void loadAll(); startPolling(); document.addEventListener('visibilitychange', onVis); });
onBeforeUnmount(() => { stopPolling(); document.removeEventListener('visibilitychange', onVis); });

// ─── chart options ──────────────────────────────────────────────────────
const palette = [
  '#2080f0', '#18a058', '#f0a020', '#d03050', '#8a2be2',
  '#0ea5e9', '#22c55e', '#fb923c', '#ef4444', '#a855f7',
  '#06b6d4', '#84cc16',
];

const protoDonutOption = computed(() => {
  const ov = overview.value;
  if (!ov) return null;
  const src = mode.value === 'bps' ? ov.proto_bps : ov.proto_pps;
  const total = src.udp + src.icmp + src.other;
  if (total === 0) return null;
  return {
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'item',
      formatter: (p: { name: string; value: number; percent: number }) =>
        `${p.name}<br/><b>${formatMetric(p.value)}</b> (${p.percent}%)`,
    },
    legend: { bottom: 8, textStyle: { color: '#aaa' } },
    series: [{
      type: 'pie',
      radius: ['50%', '78%'],
      avoidLabelOverlap: false,
      itemStyle: { borderColor: '#1a1a1f', borderWidth: 2 },
      label: { show: false },
      labelLine: { show: false },
      data: [
        { value: src.udp, name: 'UDP', itemStyle: { color: '#d03050' } },
        { value: src.icmp, name: 'ICMP', itemStyle: { color: '#f0a020' } },
        { value: src.other, name: t('analytics.proto.other'), itemStyle: { color: '#18a058' } },
      ],
    }],
  };
});

const ownerBarOption = computed(() => {
  const a = analytics.value;
  if (!a || a.top_owners.length === 0) return null;
  const rows = [...a.top_owners].reverse(); // ECharts plots bottom-up
  return {
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      formatter: (params: Array<{ name: string; value: number; data: { hosts: number } }>) => {
        const p = params[0];
        return `<b>${p.name}</b><br/>${formatMetric(p.value)}<br/>${p.data.hosts} hosts`;
      },
    },
    grid: { left: 8, right: 60, top: 8, bottom: 8, containLabel: true },
    xAxis: {
      type: 'value',
      axisLabel: { color: '#888', formatter: (v: number) => formatMetric(v) },
      splitLine: { lineStyle: { color: '#2a2a30' } },
    },
    yAxis: {
      type: 'category',
      data: rows.map((r) => r.owner),
      axisLabel: { color: '#d0d0d0', overflow: 'truncate', width: 130 },
      axisTick: { show: false },
      axisLine: { show: false },
    },
    series: [{
      type: 'bar',
      data: rows.map((r, i) => ({
        value: mode.value === 'bps' ? r.bps : r.pps,
        hosts: r.hosts,
        itemStyle: { color: palette[i % palette.length] },
      })),
      barWidth: 14,
      label: {
        show: true,
        position: 'right',
        color: '#bbb',
        formatter: (p: { value: number }) => formatMetric(p.value),
      },
    }],
  };
});

const countryBarOption = computed(() => {
  const a = analytics.value;
  if (!a || a.top_countries.length === 0) return null;
  const rows = [...a.top_countries].reverse();
  return {
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      formatter: (params: Array<{ name: string; value: number; data: { hosts: number; full: string } }>) => {
        const p = params[0];
        return `<b>${p.data.full}</b><br/>${formatMetric(p.value)}<br/>${p.data.hosts} hosts`;
      },
    },
    grid: { left: 8, right: 60, top: 8, bottom: 8, containLabel: true },
    xAxis: {
      type: 'value',
      axisLabel: { color: '#888', formatter: (v: number) => formatMetric(v) },
      splitLine: { lineStyle: { color: '#2a2a30' } },
    },
    yAxis: {
      type: 'category',
      data: rows.map((r) => `${flagEmoji(r.iso)} ${r.iso}`),
      axisLabel: { color: '#d0d0d0' },
      axisTick: { show: false },
      axisLine: { show: false },
    },
    series: [{
      type: 'bar',
      data: rows.map((r, i) => ({
        value: mode.value === 'bps' ? r.bps : r.pps,
        hosts: r.hosts,
        full: `${flagEmoji(r.iso)} ${r.name || r.iso}`,
        itemStyle: { color: palette[i % palette.length] },
      })),
      barWidth: 14,
      label: {
        show: true,
        position: 'right',
        color: '#bbb',
        formatter: (p: { value: number }) => formatMetric(p.value),
      },
    }],
  };
});

const sankeyOption = computed(() => {
  const a = analytics.value;
  if (!a || a.sankey.length === 0) return null;
  // Build the nodes list — every distinct source/target needs an entry.
  const names = new Set<string>();
  for (const e of a.sankey) { names.add(e.source); names.add(e.target); }
  const nodes = Array.from(names).map((n) => ({ name: n }));
  return {
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'item',
      triggerOn: 'mousemove',
      formatter: (p: { dataType: string; data: { source?: string; target?: string; value: number; name?: string } }) => {
        if (p.dataType === 'edge') {
          return `<b>${p.data.source}</b> → <b>${p.data.target}</b><br/>${formatMetric(p.data.value * 8)} (${p.data.value.toLocaleString()} B)`;
        }
        return `<b>${p.data.name}</b>`;
      },
    },
    series: [{
      type: 'sankey',
      layout: 'none',
      emphasis: { focus: 'adjacency' },
      nodeWidth: 16,
      nodeGap: 10,
      data: nodes,
      links: a.sankey.map((e) => ({
        source: e.source,
        target: e.target,
        value: e.bytes,
      })),
      lineStyle: { color: 'gradient', curveness: 0.5, opacity: 0.55 },
      label: { color: '#d0d0d0', fontSize: 11 },
      itemStyle: { borderColor: '#1a1a1f', borderWidth: 1 },
    }],
  };
});
</script>

<template>
  <AppLayout>
    <div class="analytics-page">
      <header class="header">
        <h1>{{ t('analytics.title') }}</h1>
        <NRadioGroup v-model:value="mode" size="small">
          <NRadio value="bps">BPS</NRadio>
          <NRadio value="pps">PPS</NRadio>
        </NRadioGroup>
      </header>

      <NAlert v-if="errored && !loadingFirst" type="warning" :show-icon="false" class="alert">
        {{ t('analytics.error') }}
      </NAlert>

      <NCard size="small" :title="t('analytics.sankey.title')" class="card sankey-card">
        <div v-if="loadingFirst" class="loading"><NSpin /></div>
        <NEmpty v-else-if="!sankeyOption" :description="t('analytics.sankey.empty')" />
        <VChart v-else :option="sankeyOption" :autoresize="true" theme="dark" class="chart sankey-chart" />
      </NCard>

      <section class="row-2col">
        <NCard size="small" :title="t('analytics.owners.title')" class="card">
          <div v-if="loadingFirst" class="loading"><NSpin /></div>
          <NEmpty v-else-if="!ownerBarOption" :description="t('analytics.owners.empty')" />
          <VChart v-else :option="ownerBarOption" :autoresize="true" theme="dark" class="chart bar-chart" />
        </NCard>

        <NCard size="small" :title="t('analytics.countries.title')" class="card">
          <div v-if="loadingFirst" class="loading"><NSpin /></div>
          <NEmpty v-else-if="!countryBarOption" :description="t('analytics.countries.empty')" />
          <VChart v-else :option="countryBarOption" :autoresize="true" theme="dark" class="chart bar-chart" />
        </NCard>
      </section>

      <section class="row-2col">
        <NCard size="small" :title="t('analytics.proto.title')" class="card">
          <div v-if="loadingFirst" class="loading"><NSpin /></div>
          <NEmpty v-else-if="!protoDonutOption" :description="t('analytics.proto.empty')" />
          <VChart v-else :option="protoDonutOption" :autoresize="true" theme="dark" class="chart donut-chart" />
        </NCard>

        <NCard size="small" :title="t('analytics.summary.title')" class="card">
          <div v-if="loadingFirst" class="loading"><NSpin /></div>
          <ul v-else class="summary-list">
            <li>
              <span class="label">{{ t('analytics.summary.owners_count') }}</span>
              <span class="value">{{ analytics?.top_owners.length ?? '—' }}</span>
            </li>
            <li>
              <span class="label">{{ t('analytics.summary.countries_count') }}</span>
              <span class="value">{{ analytics?.top_countries.length ?? '—' }}</span>
            </li>
            <li>
              <span class="label">{{ t('analytics.summary.sankey_edges') }}</span>
              <span class="value">{{ analytics?.sankey.length ?? '—' }}</span>
            </li>
            <li>
              <span class="label">{{ t('analytics.summary.active_hosts') }}</span>
              <span class="value">{{ overview?.kpis.active_hosts ?? '—' }}</span>
            </li>
            <li>
              <span class="label">{{ t('analytics.summary.total_bps') }}</span>
              <span class="value">{{ overview ? formatBps(overview.kpis.bps_now) : '—' }}</span>
            </li>
          </ul>
        </NCard>
      </section>
    </div>
  </AppLayout>
</template>

<style scoped>
.analytics-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 16px 24px 24px;
}
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.header h1 { font-size: 22px; font-weight: 600; color: #e0e0e0; margin: 0; }
.alert { margin: 0; }
.card { min-height: 240px; }
.sankey-card { min-height: 460px; }
.row-2col {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.loading {
  height: 220px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.chart { width: 100%; }
.sankey-chart { height: 420px; }
.bar-chart { height: 320px; }
.donut-chart { height: 320px; }

.summary-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.summary-list li {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  padding-bottom: 8px;
  border-bottom: 1px solid #232328;
}
.summary-list li:last-child { border-bottom: none; }
.summary-list .label { color: #888; font-size: 12px; }
.summary-list .value {
  color: #e0e0e0;
  font-size: 17px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

@media (max-width: 880px) {
  .analytics-page { padding: 12px; }
  .row-2col { grid-template-columns: 1fr; }
  .sankey-chart { height: 320px; }
}
</style>
