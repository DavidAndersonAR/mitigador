// Typed client for the Mikrotik router-management endpoints.
//
// Authenticated. The password is never returned by the server — only
// `has_password: boolean` is exposed. Send a password only on create or
// when the operator explicitly wants to rotate it on PATCH.

import { api } from '@/api/client';

export interface MikrotikRouter {
  id: number;
  name: string;
  url: string;
  username: string;
  has_password: boolean;
  verify_tls: boolean;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface CreateRouterRequest {
  name: string;
  url: string;
  username: string;
  password: string;
  verify_tls: boolean;
  enabled?: boolean;
}

export interface UpdateRouterRequest {
  name?: string;
  url?: string;
  username?: string;
  password?: string;       // omit to keep existing
  verify_tls?: boolean;
  enabled?: boolean;
}

export interface TestRouterRequest {
  url: string;
  username: string;
  password: string;
  verify_tls: boolean;
}

export interface TestRouterResponse {
  ok: boolean;
  identity?: string;
  error?: string;
}

export function fetchMikrotikRouters(): Promise<{ items: MikrotikRouter[] }> {
  return api<{ items: MikrotikRouter[] }>('/api/mikrotik/routers');
}

export function createMikrotikRouter(req: CreateRouterRequest): Promise<MikrotikRouter> {
  return api<MikrotikRouter>('/api/mikrotik/routers', {
    method: 'POST',
    body: req,
  });
}

export function updateMikrotikRouter(id: number, req: UpdateRouterRequest): Promise<MikrotikRouter> {
  return api<MikrotikRouter>(`/api/mikrotik/routers/${id}`, {
    method: 'PATCH',
    body: req,
  });
}

export function deleteMikrotikRouter(id: number): Promise<void> {
  return api<void>(`/api/mikrotik/routers/${id}`, { method: 'DELETE' });
}

export function testMikrotikRouter(req: TestRouterRequest): Promise<TestRouterResponse> {
  return api<TestRouterResponse>('/api/mikrotik/routers/test', {
    method: 'POST',
    body: req,
  });
}
