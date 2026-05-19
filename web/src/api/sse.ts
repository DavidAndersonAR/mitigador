import { fetchEventSource } from '@microsoft/fetch-event-source';

export type SSEStatus = 'connecting' | 'open' | 'reconnecting' | 'closed';

export function connectEvents(opts: {
  onOpen: () => void;
  onMessage: (type: string, data: unknown) => void;
  onError: () => void;
}): () => void {
  const ctrl = new AbortController();

  fetchEventSource('/api/events', {
    credentials: 'include',
    signal: ctrl.signal,
    onopen: async (res) => {
      if (res.ok) {
        opts.onOpen();
        return;
      }
      // Auth failure or other 4xx: stop reconnecting.
      throw new Error(`SSE open failed: ${res.status}`);
    },
    onmessage: (msg) => {
      if (!msg.event) return;
      try {
        const data = msg.data ? (JSON.parse(msg.data) as unknown) : null;
        opts.onMessage(msg.event, data);
      } catch {
        // ignore parse errors
      }
    },
    onerror: () => {
      opts.onError();
      // Returning undefined → @microsoft/fetch-event-source retries automatically.
    },
    openWhenHidden: true,
  });

  return () => ctrl.abort();
}
