<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { api, APIError } from '@/api';
import PageState from '@/components/PageState.vue';import StatusTag from '@/components/StatusTag.vue';import { useAsyncData } from '@/composables/useAsyncData';
const {data:items,loading,error,load}=useAsyncData([],api.announcements);const pending=ref(new Set<string>());const actionError=ref('');async function toggle(id:string,read:boolean){if(pending.value.has(id))return;pending.value=new Set(pending.value).add(id);actionError.value='';try{await api.setAnnouncementRead(id,read);await load()}catch(caught){actionError.value=caught instanceof APIError?caught.message:'更新失败'}finally{const next=new Set(pending.value);next.delete(id);pending.value=next}};onMounted(load);
const date=(value:string)=>new Intl.DateTimeFormat('zh-CN',{dateStyle:'medium',timeStyle:'short'}).format(new Date(value));
</script>
<template>
  <div class="page">
    <header class="page-header">
      <div>
        <p class="eyebrow">
          Announcements
        </p><h1>公告信息流</h1><p>公告正文按纯文本渲染，不执行上游 HTML。</p>
      </div><button
        class="button secondary"
        type="button"
        :disabled="loading"
        @click="load"
      >
        刷新
      </button>
    </header><p
      v-if="actionError"
      class="form-error banner"
      role="alert"
    >
      {{ actionError }}
    </p><PageState
      :loading="loading"
      :error="error"
      :empty="!loading&&!error&&items.length===0"
      empty-title="暂无公告"
      empty-text="前往站点页面手动同步，或等待服务端计划任务。"
      @retry="load"
    >
      <template #content>
        <section class="announcement-feed">
          <article
            v-for="item in items"
            :key="item.id"
            class="announcement-card"
            :class="{read:Boolean(item.readAt)}"
          >
            <header><div><strong>{{ item.siteName??item.siteId }}</strong><small>{{ item.source==='notice'?'通知':'公告' }} · {{ date(item.publishedAt??item.firstSeenAt) }}</small></div><StatusTag :status="item.readAt?'read':'unread'" /></header><p>{{ item.content }}</p><small
              v-if="item.extra"
              class="extra"
            >{{ item.extra }}</small><button
              class="button ghost compact"
              type="button"
              :disabled="pending.has(item.id)"
              @click="toggle(item.id,!item.readAt)"
            >
              {{ item.readAt?'标为未读':'标为已读' }}
            </button>
          </article>
        </section>
      </template>
    </PageState>
  </div>
</template>
