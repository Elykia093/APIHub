<script setup lang="ts">
import { computed } from 'vue';
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router';
import AppIcon from '@/components/AppIcon.vue';
import { clearSession, hasSession } from '@/session';

const route = useRoute();
const router = useRouter();
const connected = computed(() => hasSession() && route.name !== 'connect');
const links = [
  { to: '/', label: '仪表盘', icon: 'dashboard' },
  { to: '/sites', label: '站点管理', icon: 'sites' },
  { to: '/checkins', label: '签到记录', icon: 'checkins' },
  { to: '/announcements', label: '公告中心', icon: 'announcements' },
] as const;
const routeTitles: Record<string, string> = {
  announcements: '公告中心',
  checkins: '签到记录',
  overview: '仪表盘',
  'site-edit': '编辑站点',
  'site-new': '添加站点',
  sites: '站点管理',
};
const routeTitle = computed(() => routeTitles[String(route.name)] ?? '控制台');
function logout() { clearSession(); void router.replace('/connect'); }
</script>

<template>
  <div
    class="app-frame"
    :class="{ 'is-public': !connected }"
  >
    <aside
      v-if="connected"
      class="sidebar"
    >
      <RouterLink
        class="brand"
        to="/"
        aria-label="APIHub 概览"
      >
        <span class="brand-mark">A</span><span><strong>APIHub</strong><small>公益站助手</small></span>
      </RouterLink>
      <nav aria-label="主导航">
        <RouterLink
          v-for="link in links"
          :key="link.to"
          :class="{ 'is-section-active': link.to === '/sites' && route.path.startsWith('/sites/') }"
          :to="link.to"
        >
          <AppIcon
            :name="link.icon"
            :size="17"
          />
          <span>{{ link.label }}</span>
        </RouterLink>
      </nav>
      <div class="sidebar-footer">
        <div class="connection-state">
          <span aria-hidden="true" />
          <div><strong>服务已连接</strong><small>本地安全会话</small></div>
        </div>
        <button
          class="sidebar-action"
          type="button"
          @click="logout"
        >
          <AppIcon
            name="logout"
            :size="17"
          />
          <span>断开连接</span>
        </button>
      </div>
    </aside>
    <section
      v-if="connected"
      class="workspace"
    >
      <header class="topbar">
        <RouterLink
          class="topbar-brand"
          to="/"
          aria-label="APIHub 仪表盘"
        >
          <span class="brand-mark compact">A</span><strong>APIHub</strong>
        </RouterLink>
        <div class="topbar-context">
          <span>控制台</span><i aria-hidden="true">/</i><strong>{{ routeTitle }}</strong>
        </div>
        <div class="topbar-actions">
          <RouterLink
            class="topbar-button"
            to="/announcements"
          >
            <AppIcon
              name="announcements"
              :size="16"
            />
            <span>公告</span>
          </RouterLink>
          <RouterLink
            class="topbar-button primary"
            to="/sites/new"
          >
            <AppIcon
              name="plus"
              :size="16"
            />
            <span>添加站点</span>
          </RouterLink>
        </div>
      </header>
      <main class="main-content">
        <RouterView />
      </main>
    </section>
    <main
      v-else
      class="main-content"
    >
      <RouterView />
    </main>
  </div>
</template>
