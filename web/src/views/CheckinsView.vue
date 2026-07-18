<script setup lang="ts">
import { onMounted } from 'vue';
import { api } from '@/api';
import PageState from '@/components/PageState.vue';
import StatusTag from '@/components/StatusTag.vue';
import { useAsyncData } from '@/composables/useAsyncData';
const {data:runs,loading,error,load}=useAsyncData([],api.checkins);onMounted(load);
const date=(value:string)=>new Intl.DateTimeFormat('zh-CN',{dateStyle:'medium',timeStyle:'short'}).format(new Date(value));
</script>
<template>
  <div class="page">
    <header class="page-header">
      <div>
        <p class="eyebrow">
          Check-in history
        </p><h1>签到记录</h1><p>失败记录可在当天重试；成功、已签到与需人工为当天终态。</p>
      </div><button
        class="button secondary"
        type="button"
        :disabled="loading"
        @click="load"
      >
        刷新
      </button>
    </header><PageState
      :loading="loading"
      :error="error"
      :empty="!loading&&!error&&runs.length===0"
      empty-title="暂无签到记录"
      @retry="load"
    >
      <template #content>
        <div class="table-card">
          <table>
            <thead><tr><th>站点</th><th>自然日</th><th>状态</th><th>奖励</th><th>尝试</th><th>完成时间</th><th>说明</th></tr></thead><tbody>
              <tr
                v-for="run in runs"
                :key="run.id"
              >
                <td><strong>{{ run.siteName??run.siteId }}</strong></td><td>{{ run.localDate }}</td><td><StatusTag :status="run.status" /></td><td>{{ run.rewardValue??'—' }}</td><td>{{ run.attemptCount }}</td><td>{{ run.finishedAt?date(run.finishedAt):'执行中' }}</td><td class="message-cell">
                  {{ run.message||'—' }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>
    </PageState>
  </div>
</template>
