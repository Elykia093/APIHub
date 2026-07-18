<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { RouterLink } from 'vue-router';
import { api, APIError } from '@/api';
import PageState from '@/components/PageState.vue';
import StatusTag from '@/components/StatusTag.vue';
import { useAsyncData } from '@/composables/useAsyncData';

const { data: sites, loading, error, load } = useAsyncData([], api.sites);
const pending = ref(new Set<string>()); const actionError = ref('');
async function act(key:string,action:()=>Promise<unknown>){if(pending.value.has(key))return;pending.value=new Set(pending.value).add(key);actionError.value='';try{await action();await load()}catch(caught){actionError.value=caught instanceof APIError?caught.message:'操作失败'}finally{const next=new Set(pending.value);next.delete(key);pending.value=next}}
onMounted(load);
</script>

<template>
  <div class="page">
    <header class="page-header">
      <div>
        <p class="eyebrow">
          Sites
        </p><h1>站点与运行能力</h1><p>令牌不会回显；修改站点时留空即可保持原值。</p>
      </div><RouterLink
        class="button primary"
        to="/sites/new"
      >
        添加站点
      </RouterLink>
    </header><p
      v-if="actionError"
      class="form-error banner"
      role="alert"
    >
      {{ actionError }}
    </p>
    <PageState
      :loading="loading"
      :error="error"
      :empty="!loading&&!error&&sites.length===0"
      empty-title="还没有站点"
      empty-text="添加第一个公益站，服务器会按计划执行签到与公告同步。"
      @retry="load"
    >
      <RouterLink
        class="button primary"
        to="/sites/new"
      >
        添加站点
      </RouterLink><template #content>
        <section class="site-grid">
          <article
            v-for="site in sites"
            :key="site.id"
            class="site-card"
          >
            <div class="site-title">
              <div>
                <h2>{{ site.name }}</h2><a
                  :href="site.baseUrl"
                  target="_blank"
                  rel="noreferrer"
                >{{ site.baseUrl }}</a>
              </div><StatusTag :status="site.enabled ? 'enabled' : 'disabled'" />
            </div><div class="tag-row">
              <span class="text-tag">{{ site.adapter }}</span><span class="text-tag">{{ site.timezone }}</span><span class="text-tag">连续失败 {{ site.consecutiveFailures }}</span>
            </div><dl><div><dt>签到计划</dt><dd>{{ site.checkinEnabled ? site.checkinCron : '未启用' }}</dd></div><div><dt>公告计划</dt><dd>{{ site.announcementEnabled ? site.announcementCron : '未启用' }}</dd></div></dl><div class="card-actions">
              <button
                v-if="site.capabilities.checkin&&site.checkinEnabled"
                class="button primary compact"
                type="button"
                :disabled="pending.has(`checkin:${site.id}`)"
                @click="act(`checkin:${site.id}`,()=>api.runCheckin(site.id))"
              >
                立即签到
              </button><button
                v-if="site.capabilities.announcements&&site.announcementEnabled"
                class="button secondary compact"
                type="button"
                :disabled="pending.has(`sync:${site.id}`)"
                @click="act(`sync:${site.id}`,()=>api.syncAnnouncements(site.id))"
              >
                同步公告
              </button><RouterLink
                class="button ghost compact"
                :to="`/sites/${site.id}/edit`"
              >
                编辑
              </RouterLink><button
                class="button ghost compact"
                type="button"
                :disabled="pending.has(`toggle:${site.id}`)"
                @click="act(`toggle:${site.id}`,()=>api.updateSite(site.id,{enabled:!site.enabled}))"
              >
                {{ site.enabled ? '停用' : '启用' }}
              </button>
            </div>
          </article>
        </section>
      </template>
    </PageState>
  </div>
</template>
