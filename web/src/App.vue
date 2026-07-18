<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import AppShell from '@/components/AppShell.vue';

const router = useRouter();
const handleUnauthorized = () => void router.replace({ name: 'connect' });
onMounted(() => window.addEventListener('apihub:unauthorized', handleUnauthorized));
onBeforeUnmount(() => window.removeEventListener('apihub:unauthorized', handleUnauthorized));
</script>

<template><AppShell /></template>
