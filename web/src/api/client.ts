// Single fetch wrapper that includes credentials, handles CSRF, and unwraps JSON.
export interface ApiError {
  status: number;
  code: string;
}

let csrfToken: string | null = null;

async function ensureCSRF(): Promise<string> {
  if (csrfToken) return csrfToken;
  const res = await fetch('/api/auth/csrf', { credentials: 'include' });
  if (!res.ok) throw { status: res.status, code: 'csrf_fetch_failed' } as ApiError;
  const body = await res.json();
  csrfToken = body.token as string;
  return csrfToken!;
}

export function clearCSRF() {
  csrfToken = null;
}

export async function api<T>(
  path: string,
  opts: { method?: string; body?: unknown } = {},
): Promise<T> {
  const method = opts.method ?? 'GET';
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json';
  if (method !== 'GET' && method !== 'HEAD') {
    headers['X-CSRF-Token'] = await ensureCSRF();
  }
  const res = await fetch(path, {
    method,
    headers,
    credentials: 'include',
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  const body = text ? (JSON.parse(text) as unknown) : undefined;
  if (!res.ok) {
    throw {
      status: res.status,
      code: (body as { error?: string })?.error ?? 'unknown',
    } as ApiError;
  }
  return body as T;
}
