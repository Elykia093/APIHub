<script setup lang="ts">
import { onMounted } from 'vue';
import { RouterLink } from 'vue-router';
import { api } from '@/api';
import AppIcon from '@/components/AppIcon.vue';
import PageState from '@/components/PageState.vue';
import StatusTag from '@/components/StatusTag.vue';
import { useAsyncData } from '@/composables/useAsyncData';
import type { Announcement, CheckinRun, Site, Summary } from '@/types';

type Dashboard = { summary: Summary; sites: Site[]; checkins: CheckinRun[]; announcements: Announcement[] };
const initial: Dashboard = { summary:{sites:{total:0,enabled:0},today:{},unreadAnnouncements:0},sites:[],checkins:[],announcements:[] };
const { data, loading, error, load } = useAsyncData(initial, async (signal) => { const [summary,sites,checkins,announcements]=await Promise.all([api.summary(signal),api.sites(signal),api.checkins(signal),api.announcements(signal)]);return{summary,sites,checkins:checkins.slice(0,5),announcements:announcements.slice(0,4)} });
const completed=()=> (data.value.summary.today.success??0)+(data.value.summary.today.already_checked??0);
onMounted(load);
</script>

<template>
  <div class="page">
    <header class="page-header">
      <div>
        <p class="eyebrow">
          Overview
        </p><h1>概览</h1><p>站点运行、签到结果和公告状态一览。</p>
      </div><div class="page-actions">
        <RouterLink
          class="button ghost"
          to="/sites"
        >
          <AppIcon
            name="sites"
            :size="16"
          />
          管理站点
        </RouterLink><RouterLink
          class="button primary"
          to="/sites/new"
        >
          <AppIcon
            name="plus"
            :size="16"
          />
          添加站点
        </RouterLink>
      </div>
    </header>
    <PageState
      :loading="loading"
      :error="error"
      @retry="load"
    >
      <template #content>
        <section
          class="metric-grid"
          aria-label="关键指标"
        >
          <article class="metric metric-site primary-metric">
            <span class="metric-label"><AppIcon
              name="sites"
              :size="16"
            />启用站点</span><strong>{{ data.summary.sites.enabled }}</strong><small>共 {{ data.summary.sites.total }} 个站点</small>
          </article><article class="metric metric-success">
            <span class="metric-label"><AppIcon
              name="checkins"
              :size="16"
            />今日完成</span><strong>{{ completed() }}</strong><small>{{ data.summary.today.already_checked ?? 0 }} 个已签到</small>
          </article><article class="metric metric-danger">
            <span class="metric-label"><AppIcon
              name="activity"
              :size="16"
            />异常任务</span><strong>{{ data.summary.today.failed ?? 0 }}</strong><small>{{ data.summary.today.manual_required ?? 0 }} 个需人工处理</small>
          </article><article class="metric metric-info">
            <span class="metric-label"><AppIcon
              name="announcements"
              :size="16"
            />未读公告</span><strong>{{ data.summary.unreadAnnouncements }}</strong><small>来自所有已同步站点</small>
          </article>
        </section>
        <section class="dashboard-grid">
          <article class="panel">
            <div class="panel-heading">
              <div><h2>最近签到</h2><p>最近 5 条运行记录</p></div><RouterLink to="/checkins">
                查看全部
              </RouterLink>
            </div><div
              v-if="data.checkins.length"
              class="record-list"
            >
              <div
                v-for="run in data.checkins"
                :key="run.id"
                class="record-row"
              >
                <div><strong>{{ run.siteName ?? run.siteId }}</strong><small>{{ run.localDate }} · 尝试 {{ run.attemptCount }} 次</small></div><StatusTag :status="run.status" />
              </div>
            </div><p
              v-else
              class="inline-empty"
            >
              暂无签到记录。
            </p>
          </article>
          <article class="panel">
            <div class="panel-heading">
              <div><h2>最新公告</h2><p>最近 4 条公告</p></div><RouterLink to="/announcements">
                查看全部
              </RouterLink>
            </div><div
              v-if="data.announcements.length"
              class="announcement-preview"
            >
              <article
                v-for="item in data.announcements"
                :key="item.id"
              >
                <div><strong>{{ item.siteName ?? item.siteId }}</strong><StatusTag :status="item.readAt ? 'read' : 'unread'" /></div><p>{{ item.content }}</p>
              </article>
            </div><p
              v-else
              class="inline-empty"
            >
              暂无公告。
            </p>
          </article>
        </section>
      </template>
    </PageState>
  </div>
</template>
