import { createRouter, createWebHistory } from 'vue-router';
import { hasSession } from './session';

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/connect', name: 'connect', component: () => import('./views/ConnectView.vue'), meta: { public: true } },
    { path: '/', name: 'overview', component: () => import('./views/OverviewView.vue') },
    { path: '/sites', name: 'sites', component: () => import('./views/SitesView.vue') },
    { path: '/sites/new', name: 'site-new', component: () => import('./views/SiteEditorView.vue') },
    { path: '/sites/:id/edit', name: 'site-edit', component: () => import('./views/SiteEditorView.vue') },
    { path: '/checkins', name: 'checkins', component: () => import('./views/CheckinsView.vue') },
    { path: '/announcements', name: 'announcements', component: () => import('./views/AnnouncementsView.vue') },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
});

router.beforeEach((to) => {
  if (!to.meta.public && !hasSession()) return { name: 'connect', query: { redirect: to.fullPath } };
  if (to.name === 'connect' && hasSession()) return { name: 'overview' };
  return true;
});

export default router;
