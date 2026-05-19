import { defineStore } from 'pinia';
import { api } from '@/api/client';

export interface AttackEvent {
  incident_id: string;
  state: 'started' | 'updated' | 'ended';
  host_ip: string;
  vector: 'udp_flood' | 'icmp_flood';
  hostgroup: string;
  pps: number;
  bps: number;
  peak_pps: number;
  peak_bps: number;
  confidence: number;
  started_at: string;
  ended_at?: string;
  now: string;
}

export interface ActiveAttack {
  incident_id: string;
  host_ip: string;
  vector: string;
  hostgroup: string;
  pps: number;
  bps: number;
  peak_pps: number;
  peak_bps: number;
  confidence: number;
  started_at: string;
  ended: boolean;
}

interface IncidentListResponse {
  items: Array<{
    id: string;
    host_ip: string;
    vector: string;
    hostgroup: string;
    peak_pps: number;
    peak_bps: number;
    score: number;
    started_at: string;
    ended_at?: string;
  }>;
  total: number;
}

export const useIncidentsStore = defineStore('incidents', {
  state: () => ({
    active: [] as ActiveAttack[],
    sseStatus: 'connecting' as 'connecting' | 'open' | 'reconnecting' | 'closed',
  }),
  getters: {
    activeCount: (state) => state.active.filter((a) => !a.ended).length,
  },
  actions: {
    async loadActiveSnapshot() {
      try {
        const res = await api<IncidentListResponse>('/api/incidents?active=true&limit=500');
        this.active = res.items.map((i) => ({
          incident_id: i.id,
          host_ip: i.host_ip,
          vector: i.vector,
          hostgroup: i.hostgroup,
          pps: i.peak_pps,
          bps: i.peak_bps,
          peak_pps: i.peak_pps,
          peak_bps: i.peak_bps,
          confidence: i.score,
          started_at: i.started_at,
          ended: !!i.ended_at,
        }));
      } catch {
        // Keep existing state on error
      }
    },
    handleEvent(type: string, ev: AttackEvent) {
      const idx = this.active.findIndex((a) => a.incident_id === ev.incident_id);
      if (type === 'attack.started') {
        if (idx === -1) {
          this.active.unshift({
            incident_id: ev.incident_id,
            host_ip: ev.host_ip,
            vector: ev.vector,
            hostgroup: ev.hostgroup,
            pps: ev.pps,
            bps: ev.bps,
            peak_pps: ev.peak_pps,
            peak_bps: ev.peak_bps,
            confidence: ev.confidence,
            started_at: ev.started_at,
            ended: false,
          });
        }
      } else if (type === 'attack.update') {
        if (idx >= 0) {
          Object.assign(this.active[idx], {
            pps: ev.pps,
            bps: ev.bps,
            peak_pps: ev.peak_pps,
            peak_bps: ev.peak_bps,
            confidence: ev.confidence,
          });
        }
      } else if (type === 'attack.ended') {
        if (idx >= 0) {
          this.active[idx].ended = true;
        }
        // UI removes after 10s opacity fade; store keeps until view manages timeout
      }
    },
  },
});
