<script setup lang="ts">
import { ref, onMounted, computed } from 'vue';
import { useI18n } from 'vue-i18n';
import {
  NCard, NButton, NSpace, NModal, NForm, NFormItem, NInput, NSwitch,
  NAlert, NEmpty, NSpin, NPopconfirm, useMessage,
} from 'naive-ui';
import AppLayout from '@/components/AppLayout.vue';
import {
  fetchMikrotikRouters,
  createMikrotikRouter,
  updateMikrotikRouter,
  deleteMikrotikRouter,
  testMikrotikRouter,
  type MikrotikRouter,
} from '@/api/mikrotik';

const { t } = useI18n();
const message = useMessage();

const routers = ref<MikrotikRouter[]>([]);
const loading = ref(true);
const fetchError = ref(false);

interface FormState {
  id: number | null;          // null = creating
  name: string;
  url: string;
  username: string;
  password: string;
  verify_tls: boolean;
  enabled: boolean;
}

const emptyForm = (): FormState => ({
  id: null, name: '', url: 'https://', username: '', password: '',
  verify_tls: false, enabled: true,
});

const modalOpen = ref(false);
const form = ref<FormState>(emptyForm());
const formError = ref<string | null>(null);
const submitting = ref(false);
const testing = ref(false);
const testResult = ref<{ ok: boolean; text: string } | null>(null);

const isEditing = computed(() => form.value.id !== null);

async function load() {
  try {
    const resp = await fetchMikrotikRouters();
    routers.value = resp.items;
    fetchError.value = false;
  } catch {
    fetchError.value = true;
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  form.value = emptyForm();
  formError.value = null;
  testResult.value = null;
  modalOpen.value = true;
}

function openEdit(r: MikrotikRouter) {
  form.value = {
    id: r.id,
    name: r.name,
    url: r.url,
    username: r.username,
    password: '', // empty = keep existing
    verify_tls: r.verify_tls,
    enabled: r.enabled,
  };
  formError.value = null;
  testResult.value = null;
  modalOpen.value = true;
}

async function submit() {
  formError.value = null;
  if (!form.value.name.trim() || !form.value.url.trim() || !form.value.username.trim()) {
    formError.value = t('settings_mikrotik.error.required_fields');
    return;
  }
  if (!isEditing.value && !form.value.password) {
    formError.value = t('settings_mikrotik.error.password_required');
    return;
  }
  submitting.value = true;
  try {
    if (isEditing.value && form.value.id !== null) {
      await updateMikrotikRouter(form.value.id, {
        name: form.value.name,
        url: form.value.url,
        username: form.value.username,
        ...(form.value.password ? { password: form.value.password } : {}),
        verify_tls: form.value.verify_tls,
        enabled: form.value.enabled,
      });
      message.success(t('settings_mikrotik.toast.updated'));
    } else {
      await createMikrotikRouter({
        name: form.value.name,
        url: form.value.url,
        username: form.value.username,
        password: form.value.password,
        verify_tls: form.value.verify_tls,
        enabled: form.value.enabled,
      });
      message.success(t('settings_mikrotik.toast.created'));
    }
    modalOpen.value = false;
    await load();
  } catch (err: unknown) {
    const code = (err as { code?: string })?.code;
    if (code === 'name_taken') formError.value = t('settings_mikrotik.error.name_taken');
    else if (code === 'invalid_param') formError.value = t('settings_mikrotik.error.invalid_param');
    else formError.value = t('settings_mikrotik.error.generic');
  } finally {
    submitting.value = false;
  }
}

async function runTest() {
  testResult.value = null;
  if (!form.value.url || !form.value.username) {
    testResult.value = { ok: false, text: t('settings_mikrotik.error.required_fields') };
    return;
  }
  if (!form.value.password && !isEditing.value) {
    testResult.value = { ok: false, text: t('settings_mikrotik.error.password_required') };
    return;
  }
  testing.value = true;
  try {
    const resp = await testMikrotikRouter({
      url: form.value.url,
      username: form.value.username,
      password: form.value.password,  // empty when editing without password change — server will fail; ask user to type
      verify_tls: form.value.verify_tls,
    });
    if (resp.ok) {
      testResult.value = { ok: true, text: t('settings_mikrotik.test.ok', { identity: resp.identity ?? '' }) };
    } else {
      testResult.value = { ok: false, text: resp.error ?? t('settings_mikrotik.test.fail_generic') };
    }
  } catch {
    testResult.value = { ok: false, text: t('settings_mikrotik.test.fail_network') };
  } finally {
    testing.value = false;
  }
}

async function removeRouter(r: MikrotikRouter) {
  try {
    await deleteMikrotikRouter(r.id);
    message.success(t('settings_mikrotik.toast.deleted'));
    await load();
  } catch {
    message.error(t('settings_mikrotik.error.delete_failed'));
  }
}

async function toggleEnabled(r: MikrotikRouter) {
  try {
    await updateMikrotikRouter(r.id, { enabled: !r.enabled });
    await load();
  } catch {
    message.error(t('settings_mikrotik.error.generic'));
  }
}

onMounted(load);
</script>

<template>
  <AppLayout>
    <div class="settings-page">
      <header class="header">
        <h1>{{ t('settings_mikrotik.title') }}</h1>
        <NButton type="primary" @click="openCreate">+ {{ t('settings_mikrotik.add') }}</NButton>
      </header>

      <p class="hint">{{ t('settings_mikrotik.hint') }}</p>

      <NCard size="small">
        <div v-if="loading" class="loading"><NSpin /></div>
        <NAlert v-else-if="fetchError" type="warning" :show-icon="false">
          {{ t('settings_mikrotik.error.fetch') }}
        </NAlert>
        <NEmpty v-else-if="routers.length === 0" :description="t('settings_mikrotik.empty')" />
        <table v-else class="routers-table">
          <thead>
            <tr>
              <th>{{ t('settings_mikrotik.col.enabled') }}</th>
              <th>{{ t('settings_mikrotik.col.name') }}</th>
              <th>{{ t('settings_mikrotik.col.url') }}</th>
              <th>{{ t('settings_mikrotik.col.username') }}</th>
              <th>{{ t('settings_mikrotik.col.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in routers" :key="r.id">
              <td>
                <NSwitch :value="r.enabled" :round="false" @update:value="toggleEnabled(r)" />
              </td>
              <td class="name-cell">{{ r.name }}</td>
              <td class="mono">{{ r.url }}</td>
              <td class="mono">{{ r.username }}</td>
              <td>
                <NSpace size="small">
                  <NButton size="small" @click="openEdit(r)">{{ t('settings_mikrotik.edit') }}</NButton>
                  <NPopconfirm @positive-click="removeRouter(r)">
                    <template #trigger>
                      <NButton size="small" type="error" tertiary>{{ t('settings_mikrotik.delete') }}</NButton>
                    </template>
                    {{ t('settings_mikrotik.confirm_delete', { name: r.name }) }}
                  </NPopconfirm>
                </NSpace>
              </td>
            </tr>
          </tbody>
        </table>
      </NCard>

      <NModal
        v-model:show="modalOpen"
        preset="card"
        :title="isEditing ? t('settings_mikrotik.modal.edit_title') : t('settings_mikrotik.modal.create_title')"
        style="max-width: 560px;"
      >
        <NForm label-placement="top" :show-feedback="false" size="medium">
          <NFormItem :label="t('settings_mikrotik.form.name')">
            <NInput v-model:value="form.name" placeholder="BR1" />
          </NFormItem>
          <NFormItem :label="t('settings_mikrotik.form.url')">
            <NInput v-model:value="form.url" placeholder="10.100.70.1 (porta padrão 22)" />
          </NFormItem>
          <NFormItem :label="t('settings_mikrotik.form.username')">
            <NInput v-model:value="form.username" placeholder="mitigador-readonly" />
          </NFormItem>
          <NFormItem :label="isEditing ? t('settings_mikrotik.form.password_optional') : t('settings_mikrotik.form.password')">
            <NInput
              v-model:value="form.password"
              type="password"
              show-password-on="click"
              :placeholder="isEditing ? t('settings_mikrotik.form.password_keep_hint') : ''"
            />
          </NFormItem>
          <NFormItem>
            <NSpace>
              <NSwitch v-model:value="form.enabled" />
              <span>{{ t('settings_mikrotik.form.enabled') }}</span>
            </NSpace>
          </NFormItem>
        </NForm>

        <NAlert v-if="formError" type="error" :show-icon="false" style="margin-top: 8px;">
          {{ formError }}
        </NAlert>
        <NAlert
          v-if="testResult"
          :type="testResult.ok ? 'success' : 'warning'"
          :show-icon="false"
          style="margin-top: 8px;"
        >
          {{ testResult.text }}
        </NAlert>

        <template #footer>
          <NSpace justify="space-between">
            <NButton :loading="testing" @click="runTest">
              {{ t('settings_mikrotik.test.button') }}
            </NButton>
            <NSpace>
              <NButton @click="modalOpen = false">{{ t('settings_mikrotik.cancel') }}</NButton>
              <NButton type="primary" :loading="submitting" @click="submit">
                {{ isEditing ? t('settings_mikrotik.save') : t('settings_mikrotik.create') }}
              </NButton>
            </NSpace>
          </NSpace>
        </template>
      </NModal>
    </div>
  </AppLayout>
</template>

<style scoped>
.settings-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 16px 24px 24px;
}
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.header h1 { font-size: 22px; font-weight: 600; color: #e0e0e0; margin: 0; }
.hint { color: #888; font-size: 13px; margin: 0; }
.loading { padding: 32px; display: flex; justify-content: center; }

.routers-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}
.routers-table th {
  text-align: left;
  font-weight: 500;
  color: #888;
  font-size: 11px;
  text-transform: uppercase;
  padding: 8px 8px;
  border-bottom: 1px solid #2a2a30;
}
.routers-table td {
  padding: 10px 8px;
  border-bottom: 1px solid #1f1f24;
  color: #d8d8d8;
}
.routers-table tr:last-child td { border-bottom: none; }
.routers-table .name-cell { font-weight: 600; color: #e0e0e0; }
.routers-table .mono {
  font-family: 'JetBrains Mono', 'Fira Code', ui-monospace, monospace;
  font-size: 12px;
  color: #c0c0c0;
}
</style>
