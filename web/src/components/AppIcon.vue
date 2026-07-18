<script setup lang="ts">
type IconName = 'activity' | 'announcements' | 'checkins' | 'dashboard' | 'logout' | 'plus' | 'sites';

withDefaults(defineProps<{ name: IconName; size?: number }>(), { size: 18 });

const paths: Record<IconName, string[]> = {
  activity: ['M3 12h3l2-7 4 14 2-7h3l2-4 2 4'],
  announcements: ['M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9', 'M10 21h4'],
  checkins: ['M6 3v4', 'M18 3v4', 'M4 9h16', 'M5 5h14a2 2 0 0 1 2 2v12H3V7a2 2 0 0 1 2-2', 'm8 15 2 2 4-5'],
  dashboard: ['M4 4h6v6H4z', 'M14 4h6v6h-6z', 'M4 14h6v6H4z', 'M14 14h6v6h-6z'],
  logout: ['M10 17l5-5-5-5', 'M15 12H3', 'M15 4h4a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-4'],
  plus: ['M12 5v14', 'M5 12h14'],
  sites: ['M4 4h16v6H4z', 'M4 14h16v6H4z', 'M8 7h.01', 'M8 17h.01'],
};
</script>

<template>
  <svg
    aria-hidden="true"
    fill="none"
    focusable="false"
    :height="size"
    viewBox="0 0 24 24"
    :width="size"
  >
    <path
      v-for="path in paths[name]"
      :key="path"
      :d="path"
      stroke="currentColor"
      stroke-linecap="round"
      stroke-linejoin="round"
      stroke-width="1.7"
    />
  </svg>
</template>
