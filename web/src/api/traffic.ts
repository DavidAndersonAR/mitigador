// Typed client wrappers for the Plan 03 endpoints:
//   GET /api/traffic/top20            → TopTalkersResponse
//   GET /api/traffic/host/{ip}        → HostTrafficResponse  (404 if unknown)
//
// Both routes are authenticated and live inside the chi r.Group that already
// does cookie/session + CSRF. Since both are GET, no CSRF header is needed.

import { api } from '@/api/client';

export type DominantProto = 'udp' | 'icmp' | 'other';

export interface TopTalker {
  ip: string;
  hostgroup: string | null;   // null when no longest-prefix-match in the hostgroups table
  bps: number;
  pps: number;
  dominant_proto: DominantProto;
}

export interface TopTalkersResponse {
  items: TopTalker[];          // up to 20 entries, sorted bps desc
  generated_at: string;        // RFC3339 server timestamp
}

export interface HostBucket {
  // Offset in seconds from `generated_at`: 0 = now, 1 = 1s ago, ..., 59 = 59s ago.
  offset_s: number;
  pps: number;
  bps: number;
  pps_udp: number;
  bps_udp: number;
  pps_icmp: number;
  bps_icmp: number;
  pps_other: number;
  bps_other: number;
}

export interface HostTrafficResponse {
  ip: string;
  hostgroup: string | null;
  generated_at: string;
  // Newest-first: index 0 = "now" (matches aggregate.Store.Snapshot semantics).
  buckets: HostBucket[];
}

export function fetchTop20(): Promise<TopTalkersResponse> {
  return api<TopTalkersResponse>('/api/traffic/top20');
}

export function fetchHostTraffic(ip: string): Promise<HostTrafficResponse> {
  // Encode the IP since v6 addresses contain colons.
  return api<HostTrafficResponse>(`/api/traffic/host/${encodeURIComponent(ip)}`);
}
