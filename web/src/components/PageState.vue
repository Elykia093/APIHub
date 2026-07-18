<script setup lang="ts">
defineProps<{ loading?: boolean; error?: string; empty?: boolean; emptyTitle?: string; emptyText?: string }>();
defineEmits<{ retry: [] }>();
</script>

<template>
  <section
    v-if="loading"
    class="state-panel"
    aria-live="polite"
  >
    <span
      class="spinner"
      aria-hidden="true"
    /><strong>正在加载</strong><p>正在从服务器获取最新数据。</p>
  </section>
  <section
    v-else-if="error"
    class="state-panel error-state"
    role="alert"
  >
    <strong>加载失败</strong><p>{{ error }}</p><button
      class="button secondary"
      type="button"
      @click="$emit('retry')"
    >
      重试
    </button>
  </section>
  <section
    v-else-if="empty"
    class="state-panel"
  >
    <strong>{{ emptyTitle ?? '暂无数据' }}</strong><p>{{ emptyText ?? '当前没有可显示的内容。' }}</p><slot />
  </section>
  <slot
    v-else
    name="content"
  />
</template>
