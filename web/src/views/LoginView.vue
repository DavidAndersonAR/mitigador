<script setup lang="ts">
import { ref } from 'vue';
import { useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import { NCard, NForm, NFormItem, NInput, NButton, NAlert } from 'naive-ui';
import { useAuthStore } from '@/stores/auth';
import type { ApiError } from '@/api/client';

const { t } = useI18n();
const router = useRouter();
const auth = useAuthStore();

const username = ref('');
const password = ref('');
const loading = ref(false);
const errorKey = ref<string | null>(null);

async function handleSubmit() {
  if (!username.value || !password.value) return;
  loading.value = true;
  errorKey.value = null;
  try {
    await auth.login(username.value, password.value);
    router.push('/');
  } catch (err) {
    const apiErr = err as ApiError;
    if (apiErr.status === 401) {
      errorKey.value = 'login.errors.invalid_credentials';
    } else {
      errorKey.value = 'login.errors.server';
    }
    // Do NOT clear password on error (per UI-SPEC)
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <div class="login-page">
    <NCard class="login-card" :bordered="false">
      <div class="login-heading">{{ t('login.heading') }}</div>
      <div class="login-subtitle">{{ t('login.subtitle') }}</div>

      <NAlert
        v-if="errorKey"
        type="error"
        :title="t(errorKey)"
        style="margin-bottom: 16px"
        closable
        @close="errorKey = null"
      />

      <NForm @submit.prevent="handleSubmit">
        <NFormItem :label="t('login.username')" label-placement="top">
          <NInput
            v-model:value="username"
            :placeholder="t('login.username')"
            :disabled="loading"
            size="large"
            @keydown.enter="handleSubmit"
          />
        </NFormItem>
        <NFormItem :label="t('login.password')" label-placement="top">
          <NInput
            v-model:value="password"
            type="password"
            :placeholder="t('login.password')"
            :disabled="loading"
            show-password-on="click"
            size="large"
            @keydown.enter="handleSubmit"
          />
        </NFormItem>
        <NButton
          type="primary"
          block
          size="large"
          :loading="loading"
          :disabled="!username || !password"
          @click="handleSubmit"
        >
          {{ t('login.submit') }}
        </NButton>
      </NForm>
    </NCard>
  </div>
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  background: #101014;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}

.login-card {
  width: 360px;
  padding: 32px;
  background: #1c1c21;
}

.login-heading {
  font-size: 28px;
  font-weight: 600;
  color: #e0e0e0;
  margin-bottom: 4px;
  text-align: center;
}

.login-subtitle {
  font-size: 14px;
  color: #a0a0a0;
  text-align: center;
  margin-bottom: 24px;
}
</style>
