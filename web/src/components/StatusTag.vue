<script setup lang="ts">
const props = defineProps<{ status: string }>();
const labels: Record<string, string> = { running: '执行中', success: '成功', already_checked: '今日已签到', manual_required: '需人工', failed: '失败', skipped: '已跳过', partial: '部分成功', enabled: '已启用', disabled: '已停用', unread: '未读', read: '已读' };
const tones: Record<string, string> = { success: 'success', already_checked: 'success', enabled: 'success', running: 'info', partial: 'warning', manual_required: 'warning', failed: 'danger', disabled: 'neutral', skipped: 'neutral', read: 'neutral', unread: 'info' };
</script>
<template>
  <span
    class="status-tag"
    :class="`tone-${tones[props.status] ?? 'neutral'}`"
  ><span
    class="status-dot"
    aria-hidden="true"
  />{{ labels[props.status] ?? props.status }}</span>
</template>
