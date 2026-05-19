<script setup lang="ts">
import { computed } from 'vue';
import { useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import { NButton } from 'naive-ui';
import { useAuthStore } from '@/stores/auth';
import { useIncidentsStore } from '@/stores/incidents';

const { t } = useI18n();
const router = useRouter();
const auth = useAuthStore();
const incidents = useIncidentsStore();

const activeAttackCount = computed(() => incidents.active.filter((a) => !a.ended).length);

const menuOptions = computed(() => [
  {
    label: t('nav.live'),
    key: 'dashboard',
    badge: activeAttackCount.value > 0 ? activeAttackCount.value : undefined,
  },
  {
    label: t('nav.flows'),
    key: 'flows',
  },
  {
    label: t('nav.analytics'),
    key: 'analytics',
  },
  {
    label: t('nav.traffic'),
    key: 'traffic',
  },
  {
    label: t('nav.exporters'),
    key: 'exporters',
  },
  {
    label: t('nav.bgp'),
    key: 'bgp',
  },
  {
    label: t('nav.incidents'),
    key: 'incidents',
  },
]);

const routeMap: Record<string, string> = {
  dashboard: '/',
  flows: '/flows',
  analytics: '/analytics',
  traffic: '/traffic',
  exporters: '/exporters',
  bgp: '/bgp',
  incidents: '/incidents',
};

async function handleLogout() {
  await auth.logout();
  router.push({ name: 'login' });
}
</script>

<template>
  <div class="sidebar-nav">
    <div class="sidebar-brand">
      <span class="brand-text">{{ t('app.title') }}</span>
    </div>

    <nav class="sidebar-menu">
      <router-link
        v-for="opt in menuOptions"
        :key="opt.key"
        :to="routeMap[opt.key]"
        class="nav-item"
        active-class="nav-item--active"
      >
        <span class="nav-label">{{ opt.label }}</span>
        <span v-if="opt.badge" class="nav-badge">{{ opt.badge }}</span>
      </router-link>
    </nav>

    <div class="sidebar-footer">
      <span class="sidebar-username">{{ auth.user?.username }}</span>
      <NButton text size="small" @click="handleLogout" class="logout-btn">
        {{ t('nav.logout') }}
      </NButton>
    </div>
  </div>
</template>

<style scoped>
.sidebar-nav {
  width: 240px;
  height: 100%;
  background: #1c1c21;
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
}

.sidebar-brand {
  padding: 24px 16px 16px;
  border-bottom: 1px solid #2a2a30;
}

.brand-text {
  font-size: 18px;
  font-weight: 600;
  color: #e0e0e0;
}

.sidebar-menu {
  flex: 1;
  overflow-y: auto;
  padding: 8px 0;
}

.nav-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  color: #a0a0a0;
  text-decoration: none;
  font-size: 14px;
  border-left: 3px solid transparent;
  transition: color 0.15s, border-color 0.15s, background-color 0.15s;
  min-height: 44px;
  box-sizing: border-box;
}

.nav-item:hover {
  color: #e0e0e0;
  background-color: rgba(255, 255, 255, 0.04);
}

.nav-item--active {
  color: #18a058;
  border-left-color: #18a058;
  background-color: rgba(24, 160, 88, 0.08);
}

.nav-label {
  flex: 1;
}

.nav-badge {
  background-color: #d03050;
  color: #fff;
  border-radius: 10px;
  padding: 1px 7px;
  font-size: 11px;
  font-weight: 600;
  min-width: 18px;
  text-align: center;
  line-height: 18px;
}

.sidebar-footer {
  padding: 16px;
  border-top: 1px solid #2a2a30;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.sidebar-username {
  font-size: 13px;
  color: #a0a0a0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 140px;
}

.logout-btn {
  color: #a0a0a0;
  font-size: 13px;
  flex-shrink: 0;
}

.logout-btn:hover {
  color: #e0e0e0;
}
</style>
