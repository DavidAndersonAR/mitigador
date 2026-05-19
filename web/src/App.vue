<script setup lang="ts">
import { NConfigProvider, NMessageProvider, darkTheme, type GlobalThemeOverrides } from 'naive-ui';

// Match UI-SPEC §Color: green accent, dark theme defaults.
const themeOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor: '#18a058',
    primaryColorHover: '#36ad6a',
    primaryColorPressed: '#0c7a43',
    primaryColorSuppl: '#36ad6a',
    warningColor: '#f0a020',
    warningColorHover: '#fcb040',
    warningColorPressed: '#c97c10',
    warningColorSuppl: '#fcb040',
    errorColor: '#d03050',
    errorColorHover: '#de576d',
    errorColorPressed: '#ab1f3f',
    errorColorSuppl: '#de576d',
    bodyColor: '#101014',
    cardColor: '#1c1c21',
    modalColor: '#1c1c21',
    popoverColor: '#1c1c21',
  },
};
</script>

<template>
  <NConfigProvider :theme="darkTheme" :theme-overrides="themeOverrides">
    <NMessageProvider>
      <router-view />
    </NMessageProvider>
  </NConfigProvider>
</template>

<style>
html,
body,
#app {
  height: 100%;
  margin: 0;
  padding: 0;
}
body {
  background: #101014;
  color: #e0e0e0;
  font-family: Inter, system-ui, sans-serif;
}
</style>
