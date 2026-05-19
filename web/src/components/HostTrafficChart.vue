<script setup lang="ts">
import { ref, watch, onMounted, onBeforeUnmount, onUnmounted } from 'vue';
import { useI18n } from 'vue-i18n';
import { NEmpty } from 'naive-ui';
import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';
import type { HostBucket } from '@/api/traffic';

// Series colors picked from Naive UI dark theme palette so the chart fits the
// existing dashboard. Documented hex values (do NOT change without updating
// DashboardView too):
//   UDP   → #d03050 (red — same as the active-attack accent in DashboardView)
//   ICMP  → #f0a020 (amber — same as the medium-confidence accent)
//   Other → #18a058 (green — same as the success accent)
// Fill is the stroke at 28% opacity for the stacked-area look.
const COLOR_UDP_STROKE   = '#d03050';
const COLOR_UDP_FILL     = 'rgba(208, 48, 80, 0.28)';
const COLOR_ICMP_STROKE  = '#f0a020';
const COLOR_ICMP_FILL    = 'rgba(240, 160, 32, 0.28)';
const COLOR_OTHER_STROKE = '#18a058';
const COLOR_OTHER_FILL   = 'rgba(24, 160, 88, 0.28)';
const COLOR_AXIS         = '#a0a0a0';
const COLOR_GRID         = '#2a2a30';

const props = defineProps<{ buckets: HostBucket[]; mode: 'bps' | 'pps'; height?: number }>();

const { t } = useI18n();

const containerRef = ref<HTMLDivElement | null>(null);
let chart: uPlot | null = null;
let ro: ResizeObserver | null = null;

// ---------------------------------------------------------------------------
// Y-axis / tooltip formatter
// ---------------------------------------------------------------------------
function formatY(v: number): string {
  if (props.mode === 'bps') {
    if (v >= 1e9) return (v / 1e9).toFixed(1) + ' Gbps';
    if (v >= 1e6) return (v / 1e6).toFixed(1) + ' Mbps';
    if (v >= 1e3) return (v / 1e3).toFixed(1) + ' Kbps';
    return v.toFixed(0) + ' bps';
  }
  // pps
  if (v >= 1e6) return (v / 1e6).toFixed(1) + 'M';
  if (v >= 1e3) return (v / 1e3).toFixed(1) + 'K';
  return v.toFixed(0);
}

// ---------------------------------------------------------------------------
// Build uPlot AlignedData from props.buckets.
//
// Bucket index 0 = newest. uPlot expects X ascending left→right, so we
// reverse the array so "now" ends up on the right edge (most intuitive
// for an operator watching live traffic).
//
// Stacked-area encoding (cumulative sums):
//   y1 = udp
//   y2 = udp + icmp
//   y3 = udp + icmp + other  (total)
// uPlot bands fill between adjacent series for the stacked look.
// ---------------------------------------------------------------------------
function buildData(): uPlot.AlignedData {
  const n = props.buckets.length;
  const xs: number[]        = new Array(n);
  const udp: number[]       = new Array(n);
  const udpIcmp: number[]   = new Array(n);
  const total: number[]     = new Array(n);

  for (let i = 0; i < n; i++) {
    // src[n-1-i] = oldest-first when i=0, newest when i=n-1
    const src = props.buckets[n - 1 - i];
    // X axis: seconds offset from now; rightmost point = 0 (now)
    xs[i] = -(src.offset_s);
    const u  = props.mode === 'bps' ? src.bps_udp  : src.pps_udp;
    const ic = props.mode === 'bps' ? src.bps_icmp : src.pps_icmp;
    const ot = props.mode === 'bps' ? src.bps_other : src.pps_other;
    udp[i]     = u;
    udpIcmp[i] = u + ic;
    total[i]   = u + ic + ot;
  }

  // Cast required: TypeScript sees number[] but AlignedData allows (number|null)[]
  return [xs, udp, udpIcmp, total] as uPlot.AlignedData;
}

// ---------------------------------------------------------------------------
// Build uPlot Options
// ---------------------------------------------------------------------------
function buildOptions(width: number, height: number): uPlot.Options {
  const tUDP   = t('traffic.chart.series.udp');
  const tICMP  = t('traffic.chart.series.icmp');
  const tOther = t('traffic.chart.series.other');
  const tTime  = t('traffic.chart.axis.time');
  const tY     = props.mode === 'bps'
    ? t('traffic.chart.axis.bps')
    : t('traffic.chart.axis.pps');

  return {
    width,
    height,
    cursor: {
      drag: { x: false, y: false },
      points: { size: 6 },
    },
    legend: { show: false },
    scales: {
      x: { time: false },
      y: { auto: true },
    },
    axes: [
      {
        // X axis
        stroke: COLOR_AXIS,
        grid:   { stroke: COLOR_GRID, width: 1 },
        ticks:  { stroke: COLOR_GRID },
        label:   tTime,
        labelGap: 6,
        // Axis.Values: DynamicValues signature
        values: (_u: uPlot, splits: number[]) =>
          splits.map((s) => (s === 0 ? '0' : String(s) + 's')) as (string | null)[],
      },
      {
        // Y axis
        stroke: COLOR_AXIS,
        grid:   { stroke: COLOR_GRID, width: 1 },
        ticks:  { stroke: COLOR_GRID },
        label:   tY,
        labelGap: 6,
        values: (_u: uPlot, splits: number[]) =>
          splits.map((s) => formatY(s)) as (string | null)[],
      },
    ],
    series: [
      {}, // x — required placeholder for X series
      {
        label:  tUDP,
        stroke: COLOR_UDP_STROKE,
        fill:   COLOR_UDP_FILL,
        width:  1.5,
        // Series.Value: (self, rawValue, seriesIdx, idx) => string
        value: (_u: uPlot, v: number) => formatY(v ?? 0),
      },
      {
        label:  tICMP,
        stroke: COLOR_ICMP_STROKE,
        fill:   COLOR_ICMP_FILL,
        width:  1.5,
        value: (_u: uPlot, v: number) => formatY(v ?? 0),
      },
      {
        label:  tOther,
        stroke: COLOR_OTHER_STROKE,
        fill:   COLOR_OTHER_FILL,
        width:  1.5,
        value: (_u: uPlot, v: number) => formatY(v ?? 0),
      },
    ],
    bands: [
      // Fill between adjacent series to create the stacked-area effect.
      // Band.Bounds = [aboveSeriesIdx, belowSeriesIdx]
      { series: [2, 1] as uPlot.Band.Bounds }, // ICMP-stack fills above UDP-stack
      { series: [3, 2] as uPlot.Band.Bounds }, // Other-stack fills above ICMP-stack
    ],
  };
}

// ---------------------------------------------------------------------------
// Chart lifecycle helpers
// ---------------------------------------------------------------------------
function mountChart(): void {
  if (!containerRef.value) return;
  const w = containerRef.value.clientWidth || 600;
  const h = props.height ?? 280;
  chart = new uPlot(buildOptions(w, h), buildData(), containerRef.value);
}

function destroyChart(): void {
  if (chart) {
    chart.destroy();
    chart = null;
  }
}

function handleResize(): void {
  if (chart && containerRef.value) {
    chart.setSize({
      width:  containerRef.value.clientWidth,
      height: props.height ?? 280,
    });
  }
}

// ---------------------------------------------------------------------------
// Vue lifecycle
// ---------------------------------------------------------------------------
onMounted(() => {
  if (props.buckets.length === 0) return; // empty state handled by template
  mountChart();
  if (typeof ResizeObserver !== 'undefined' && containerRef.value) {
    ro = new ResizeObserver(handleResize);
    ro.observe(containerRef.value);
  }
});

// Re-init when `mode` flips — axis labels and scale units change, so we need
// a fresh opts object; a simple setData won't update the axis label.
watch(
  () => props.mode,
  () => {
    destroyChart();
    if (props.buckets.length > 0) mountChart();
  },
);

// For bucket-only updates: use setData to avoid full re-init flicker.
watch(
  () => props.buckets,
  () => {
    if (props.buckets.length === 0) {
      destroyChart();
      return;
    }
    if (!chart) {
      mountChart();
      return;
    }
    chart.setData(buildData());
  },
  { deep: true },
);

onBeforeUnmount(() => {
  if (ro && containerRef.value) ro.unobserve(containerRef.value);
  ro = null;
});

onUnmounted(() => {
  destroyChart();
});
</script>

<template>
  <div class="host-traffic-chart">
    <div v-if="buckets.length === 0" class="empty-state">
      <NEmpty :description="t('traffic.chart.empty')" />
    </div>
    <div
      v-else
      ref="containerRef"
      class="chart-container"
      :style="{ height: (height ?? 280) + 'px' }"
    ></div>
  </div>
</template>

<style scoped>
.host-traffic-chart {
  width: 100%;
}

.chart-container {
  width: 100%;
}

.empty-state {
  min-height: 200px;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* uPlot's stylesheet is dark-theme-agnostic; nudge text color for legibility
   against the dashboard's dark background (#101014 / #1c1c21). */
:deep(.u-legend) {
  color: #c0c0c0;
}

:deep(.u-title) {
  color: #e0e0e0;
}
</style>
