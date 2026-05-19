<script setup lang="ts">
// Placeholder — implemented in Task 2
import { useIncidentsStore } from '@/stores/incidents';
const incidents = useIncidentsStore();
</script>
<template>
  <span :data-sse-status="incidents.sseStatus">SSE</span>
</template>
