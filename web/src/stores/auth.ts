import { defineStore } from 'pinia';
import { api } from '@/api/client';

interface User {
  id: number;
  username: string;
  email: string;
}

export const useAuthStore = defineStore('auth', {
  state: () => ({ user: null as User | null, loaded: false }),
  actions: {
    async ensureLoaded() {
      if (this.loaded) return;
      try {
        this.user = await api<User>('/api/auth/me');
      } catch {
        this.user = null;
      }
      this.loaded = true;
    },
    async login(username: string, password: string) {
      await api('/api/auth/login', { method: 'POST', body: { username, password } });
      this.user = await api<User>('/api/auth/me');
    },
    async logout() {
      try {
        await api('/api/auth/logout', { method: 'POST' });
      } finally {
        this.user = null;
        this.loaded = false;
      }
    },
  },
});
