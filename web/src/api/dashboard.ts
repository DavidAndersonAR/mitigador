// Typed client wrappers for the Flow Dashboard endpoints.
//   GET /api/dashboard/overview        → DashboardOverview (KPIs + 60s timeseries + top-10)
//   GET /api/dashboard/recent?n=NN     → DashboardRecent  (latest flow records, src→dst)

import { api } from '@/api/client';

export type DashboardProto = 'udp' | 'tcp' | 'icmp' | 'other';
export type DashboardDominantProto = 'udp' | 'icmp' | 'other';

export interface DashboardBucket {
  pps: number;
  bps: number;
  pps_udp: number;
  bps_udp: number;
  pps_icmp: number;
  bps_icmp: number;
  pps_other: number;
  bps_other: number;
}

export interface DashboardProtoBreakdown {
  udp: number;
  icmp: number;
  other: number;
}

export interface DashboardKPIs {
  bps_now: number;
  pps_now: number;
  bps_avg: number;
  pps_avg: number;
  active_hosts: number;
  exporters_online: number;
  exporters_total: number;
}

export interface DashboardTopEntry {
  ip: string;
  hostname: string;       // PTR — empty until cache populates
  owner: string;          // ASN holder (e.g. "Cloudflare") — empty if unknown
  hostgroup: string | null;
  bps: number;
  pps: number;
  dominant_proto: DashboardDominantProto;
  sparkline: number[]; // bps per second, newest-first, length = 60
}

export interface DashboardOverview {
  generated_at: string;
  kpis: DashboardKPIs;
  buckets: DashboardBucket[]; // newest-first, length = 60
  proto_bps: DashboardProtoBreakdown;
  proto_pps: DashboardProtoBreakdown;
  top: DashboardTopEntry[];
}

export interface DashboardRecentFlow {
  received_ms: number;
  src_ip: string;
  src_hostname: string;
  src_owner: string;
  dst_ip: string;
  dst_hostname: string;
  dst_owner: string;
  dst_hostgroup: string;
  proto: DashboardProto;
  bytes: number;
  packets: number;
  avg_pkt_bytes: number;
  sample_rate: number;
  exporter: string;
}

export interface DashboardRecent {
  generated_at: string;
  flows: DashboardRecentFlow[];
}

export function fetchOverview(): Promise<DashboardOverview> {
  return api<DashboardOverview>('/api/dashboard/overview');
}

export function fetchRecent(n = 50): Promise<DashboardRecent> {
  return api<DashboardRecent>(`/api/dashboard/recent?n=${encodeURIComponent(String(n))}`);
}
