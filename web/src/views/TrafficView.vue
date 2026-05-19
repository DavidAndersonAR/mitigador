<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue';
import { useI18n } from 'vue-i18n';
import { NEmpty, NSpin, NAlert, NButton, NTag, NDrawer, NDrawerContent, NSpace, NRadioGroup, NRadio } from 'naive-ui';
import AppLayout from '@/components/AppLayout.vue';
import HostTrafficChart from '@/components/HostTrafficChart.vue';
import { fetchTop20, fetchHostTraffic, type TopTalker, type HostBucket, type DominantProto } from '@/api/traffic';

// ──────────────────────────────────────────────────────────────────────────
// State
// ──────────────────────────────────────────────────────────────────────────
const { t } = useI18n();

const rows = ref<TopTalker[]>([]);
const loadingFirst = ref(true);
const topError = ref(false);
const lastTopFetch = ref<number>(0);

const drawerOpen = ref(false);
const drawerIP = ref<string | null>(null);
const drawerHostgroup = ref<string | null>(null);
const drawerBuckets = ref<HostBucket[]>([]);
const drawerLoading = ref(false);
const drawerError = ref<'fetch' | 'not_found' | null>(null);
const mode = ref<'bps' | 'pps'>('bps');

const isMobile = ref(false);
let topInterval: ReturnType<typeof setInterval> | null = null;
let hostInterval: ReturnType<typeof setInterval> | null = null;

// ──────────────────────────────────────────────────────────────────────────
// Formatters (mirrored from DashboardView for visual consistency)
// ──────────────────────────────────────────────────────────────────────────
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
function protoTagType(proto: DominantProto): 'error' | 'warning' | 'success' {
  // Reuse the dashboard accent semantics for cross-view consistency.
  if (proto === 'udp') return 'error';
  if (proto === 'icmp') return 'warning';
  return 'success';
}
function protoLabel(proto: DominantProto): string {
  return t('traffic.proto.' + proto);
}
function hostgroupLabel(hg: string | null): string {
  return hg ?? t('traffic.hostgroup.none');
}

// ──────────────────────────────────────────────────────────────────────────
// Data fetching
// ──────────────────────────────────────────────────────────────────────────
async function loadTop20() {
  try {
    const resp = await fetchTop20();
    rows.value = resp.items;
    topError.value = false;
    lastTopFetch.value = Date.now();
  } catch {
    topError.value = true;
  } finally {
    loadingFirst.value = false;
  }
}

async function loadHost(ip: string) {
  drawerLoading.value = true;
  drawerError.value = null;
  try {
    const resp = await fetchHostTraffic(ip);
    drawerBuckets.value = resp.buckets;
    drawerHostgroup.value = resp.hostgroup;
  } catch (err: unknown) {
    // ApiError shape: { status, code }
    const status = (err as { status?: number })?.status;
    drawerError.value = status === 404 ? 'not_found' : 'fetch';
    drawerBuckets.value = [];
  } finally {
    drawerLoading.value = false;
  }
}

// ──────────────────────────────────────────────────────────────────────────
// Polling lifecycle
// ──────────────────────────────────────────────────────────────────────────
function startTopPolling() {
  if (topInterval !== null) return;
  topInterval = setInterval(() => { void loadTop20(); }, 1000);
}
function stopTopPolling() {
  if (topInterval !== null) { clearInterval(topInterval); topInterval = null; }
}
function startHostPolling() {
  if (hostInterval !== null) return;
  if (!drawerIP.value) return;
  hostInterval = setInterval(() => {
    if (drawerIP.value) void loadHost(drawerIP.value);
  }, 1000);
}
function stopHostPolling() {
  if (hostInterval !== null) { clearInterval(hostInterval); hostInterval = null; }
}

function onVisibilityChange() {
  if (document.visibilityState === 'visible') {
    // Resume both pollers; do an immediate refresh so the UI catches up.
    void loadTop20();
    startTopPolling();
    if (drawerOpen.value && drawerIP.value) {
      void loadHost(drawerIP.value);
      startHostPolling();
    }
  } else {
    stopTopPolling();
    stopHostPolling();
  }
}

// ──────────────────────────────────────────────────────────────────────────
// Drawer interactions
// ──────────────────────────────────────────────────────────────────────────
function openDrawer(row: TopTalker) {
  drawerIP.value = row.ip;
  drawerHostgroup.value = row.hostgroup;
  drawerBuckets.value = [];
  drawerError.value = null;
  drawerOpen.value = true;
  void loadHost(row.ip);
  startHostPolling();
}
function closeDrawer() {
  drawerOpen.value = false;
  drawerIP.value = null;
  drawerBuckets.value = [];
  stopHostPolling();
}
function retryTop() {
  topError.value = false;
  loadingFirst.value = true;
  void loadTop20();
}
function retryHost() {
  if (drawerIP.value) void loadHost(drawerIP.value);
}

// ──────────────────────────────────────────────────────────────────────────
// Mobile detection (mirrors DashboardView pattern)
// ──────────────────────────────────────────────────────────────────────────
function updateMobile() {
  isMobile.value = window.innerWidth < 640;
}

// ──────────────────────────────────────────────────────────────────────────
// Lifecycle
// ──────────────────────────────────────────────────────────────────────────
onMounted(async () => {
  updateMobile();
  window.addEventListener('resize', updateMobile);
  document.addEventListener('visibilitychange', onVisibilityChange);
  await loadTop20();
  startTopPolling();
});

onBeforeUnmount(() => {
  stopTopPolling();
  stopHostPolling();
  window.removeEventListener('resize', updateMobile);
  document.removeEventListener('visibilitychange', onVisibilityChange);
});

// ──────────────────────────────────────────────────────────────────────────
// Drawer title (computed so i18n + IP stay in sync)
// ──────────────────────────────────────────────────────────────────────────
const drawerTitle = computed(() => t('traffic.drawer.title', { ip: drawerIP.value ?? '' }));
</script>

<template>
  <AppLayout>
    <template #header-title>
      <h1 class="page-title">{{ t('traffic.title') }}</h1>
    </template>

    <p class="subtitle">{{ t('traffic.subtitle') }}</p>

    <!-- First-load spinner -->
    <div v-if="loadingFirst" class="state-center">
      <NSpin size="large" />
    </div>

    <!-- Persistent error banner (live polls continue; this only shows on stale fetch) -->
    <NAlert
      v-else-if="topError"
      type="error"
      :title="t('traffic.error.fetch')"
      style="margin-bottom: 16px"
    >
      <template #default>
        <NButton text @click="retryTop">{{ t('actions.retry') }}</NButton>
      </template>
    </NAlert>

    <!-- Main content -->
    <template v-else>
      <!-- Empty -->
      <div v-if="rows.length === 0" class="state-center">
        <NEmpty :description="t('traffic.empty.title')">
          <template #extra>
            <span class="empty-body">{{ t('traffic.empty.body') }}</span>
          </template>
        </NEmpty>
      </div>

      <!-- Desktop table -->
      <div v-else-if="!isMobile" class="table-wrapper">
        <table class="t-table">
          <thead>
            <tr>
              <th>{{ t('traffic.col.ip') }}</th>
              <th>{{ t('traffic.col.hostgroup') }}</th>
              <th class="right">{{ t('traffic.col.bps') }}</th>
              <th class="right">{{ t('traffic.col.pps') }}</th>
              <th>{{ t('traffic.col.proto') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="row in rows"
              :key="row.ip"
              class="t-row"
              tabindex="0"
              @click="openDrawer(row)"
              @keydown.enter="openDrawer(row)"
            >
              <td class="mono">{{ row.ip }}</td>
              <td>{{ hostgroupLabel(row.hostgroup) }}</td>
              <td class="mono right">{{ formatBps(row.bps) }}</td>
              <td class="mono right">{{ formatPps(row.pps) }}</td>
              <td>
                <NTag :type="protoTagType(row.dominant_proto)" size="small" round>
                  {{ protoLabel(row.dominant_proto) }}
                </NTag>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Mobile cards -->
      <div v-else class="mobile-cards">
        <div
          v-for="row in rows"
          :key="row.ip"
          class="mobile-card"
          tabindex="0"
          @click="openDrawer(row)"
          @keydown.enter="openDrawer(row)"
        >
          <div class="mobile-line1">
            <span class="mono">{{ row.ip }}</span>
            <NTag :type="protoTagType(row.dominant_proto)" size="small" round>
              {{ protoLabel(row.dominant_proto) }}
            </NTag>
          </div>
          <div class="mobile-line2">
            <span class="mono">{{ formatBps(row.bps) }}</span>
            <span class="mono">{{ formatPps(row.pps) }}</span>
          </div>
          <div class="mobile-line3">{{ hostgroupLabel(row.hostgroup) }}</div>
        </div>
      </div>
    </template>

    <!-- Drawer with per-host chart -->
    <NDrawer
      v-model:show="drawerOpen"
      :width="isMobile ? '100%' : 560"
      placement="right"
      :on-mask-click="closeDrawer"
      :on-esc="closeDrawer"
    >
      <NDrawerContent :title="drawerTitle" closable @close="closeDrawer">
        <NSpace vertical :size="16">
          <!-- Mode toggle -->
          <NRadioGroup v-model:value="mode" size="small">
            <NRadio value="bps">{{ t('traffic.toggle.bps') }}</NRadio>
            <NRadio value="pps">{{ t('traffic.toggle.pps') }}</NRadio>
          </NRadioGroup>

          <!-- Loading -->
          <div v-if="drawerLoading && drawerBuckets.length === 0" class="state-center">
            <NSpin size="medium" />
          </div>

          <!-- Error: not_found (host went stale while drawer was open) -->
          <NAlert
            v-else-if="drawerError === 'not_found'"
            type="warning"
            :title="t('traffic.host.not_found')"
          />

          <!-- Error: fetch -->
          <NAlert
            v-else-if="drawerError === 'fetch'"
            type="error"
            :title="t('traffic.error.fetch')"
          >
            <template #default>
              <NButton text @click="retryHost">{{ t('actions.retry') }}</NButton>
            </template>
          </NAlert>

          <!-- Chart -->
          <HostTrafficChart
            v-else
            :buckets="drawerBuckets"
            :mode="mode"
            :height="280"
          />

          <!-- Hostgroup label under chart -->
          <div class="drawer-meta">
            <span class="meta-label">{{ t('traffic.col.hostgroup') }}:</span>
            <span class="meta-value">{{ hostgroupLabel(drawerHostgroup) }}</span>
          </div>
        </NSpace>
      </NDrawerContent>
    </NDrawer>
  </AppLayout>
</template>

<style scoped>
.page-title { font-size: 20px; font-weight: 600; color: #e0e0e0; margin: 0; }
.subtitle { font-size: 13px; color: #a0a0a0; margin: 0 0 16px 0; }

.state-center { display: flex; align-items: center; justify-content: center; min-height: 200px; }
.empty-body { font-size: 13px; color: #a0a0a0; max-width: 360px; text-align: center; display: block; margin-top: 8px; }

.table-wrapper { overflow-x: auto; border-radius: 8px; background: #1c1c21; }
.t-table { width: 100%; border-collapse: collapse; font-size: 14px; }
.t-table th {
  padding: 10px 14px;
  font-size: 12px;
  font-weight: 600;
  color: #a0a0a0;
  text-align: left;
  border-bottom: 1px solid #2a2a30;
  white-space: nowrap;
}
.t-table td {
  padding: 10px 14px;
  border-bottom: 1px solid #1e1e24;
  color: #e0e0e0;
}
.t-row { cursor: pointer; transition: background-color 0.15s; }
.t-row:hover td { background-color: rgba(255, 255, 255, 0.04); }
.t-row:focus { outline: 2px solid #18a058; outline-offset: -2px; }
.mono { font-family: 'JetBrains Mono', 'Fira Code', ui-monospace, monospace; }
.right { text-align: right; }

.mobile-cards { display: flex; flex-direction: column; gap: 8px; }
.mobile-card {
  background: #1c1c21;
  border-radius: 8px;
  padding: 12px 16px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  cursor: pointer;
}
.mobile-line1 { display: flex; align-items: center; justify-content: space-between; font-size: 14px; font-weight: 600; color: #e0e0e0; }
.mobile-line2 { display: flex; gap: 16px; font-size: 13px; color: #c0c0c0; }
.mobile-line3 { font-size: 12px; color: #a0a0a0; }

.drawer-meta { display: flex; gap: 8px; font-size: 13px; color: #a0a0a0; padding-top: 8px; border-top: 1px solid #2a2a30; }
.meta-label { font-weight: 600; }
.meta-value { color: #e0e0e0; font-family: 'JetBrains Mono', ui-monospace, monospace; }
</style>
