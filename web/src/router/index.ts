import { createRouter, createWebHistory } from 'vue-router';
import LoginView from '@/views/LoginView.vue';
import DashboardView from '@/views/DashboardView.vue';
import ExportersView from '@/views/ExportersView.vue';
import TrafficView from '@/views/TrafficView.vue';
import FlowsDashboardView from '@/views/FlowsDashboardView.vue';
import BGPStubView from '@/views/BGPStubView.vue';
import IncidentListView from '@/views/IncidentListView.vue';
import IncidentDetailView from '@/views/IncidentDetailView.vue';
import { useAuthStore } from '@/stores/auth';

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: LoginView, meta: { public: true } },
    { path: '/', name: 'dashboard', component: DashboardView },
    { path: '/flows', name: 'flows', component: FlowsDashboardView },
    { path: '/traffic', name: 'traffic', component: TrafficView },
    { path: '/exporters', name: 'exporters', component: ExportersView },
    { path: '/bgp', name: 'bgp', component: BGPStubView },
    { path: '/incidents', name: 'incidents', component: IncidentListView },
    { path: '/incidents/:id', name: 'incident', component: IncidentDetailView },
  ],
});

router.beforeEach(async (to) => {
  if (to.meta.public) return true;
  const auth = useAuthStore();
  await auth.ensureLoaded();
  if (!auth.user) return { name: 'login' };
});
