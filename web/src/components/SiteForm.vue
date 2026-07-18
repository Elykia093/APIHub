<script setup lang="ts">
import { computed, reactive, watch } from 'vue';
import type { AdapterDescriptor, Site, SiteWrite } from '@/types';

const props = defineProps<{ site: Site | undefined; adapters: AdapterDescriptor[]; submitting: boolean }>();
const emit = defineEmits<{ submit: [value: SiteWrite]; cancel: [] }>();
type SiteFormState = Omit<SiteWrite, 'checkinEnabled' | 'announcementEnabled'> & { checkinEnabled: boolean; announcementEnabled: boolean };
const emptyForm = (): SiteFormState => ({ name:'',baseUrl:'',adapter:'auto',userId:'',accessToken:'',enabled:true,checkinEnabled:false,announcementEnabled:false,checkinCron:'15 8 * * *',announcementCron:'*/30 * * * *',timezone:'Asia/Shanghai' });
const form = reactive<SiteFormState>(emptyForm());
const editing = computed(() => Boolean(props.site));
const selected = computed(() => props.adapters.find((item) => item.name === form.adapter));

watch(() => props.site, (site) => { Object.assign(form,site?{name:site.name,baseUrl:site.baseUrl,adapter:site.adapter,userId:site.userId,accessToken:'',enabled:site.enabled,checkinEnabled:site.checkinEnabled,announcementEnabled:site.announcementEnabled,checkinCron:site.checkinCron,announcementCron:site.announcementCron,timezone:site.timezone}:emptyForm()); }, { immediate:true });
watch(selected, (adapter) => { if (!adapter) return; if (!adapter.capabilities.checkin) form.checkinEnabled=false; else if (!editing.value) form.checkinEnabled=true; if (!adapter.capabilities.announcements) form.announcementEnabled=false; else if (!editing.value) form.announcementEnabled=true; if (!adapter.capabilities.requiresUserId && !editing.value) form.userId=''; });
function submit(){const payload:SiteWrite={...form};if(!editing.value&&payload.adapter==='auto'){delete payload.checkinEnabled;delete payload.announcementEnabled}if(editing.value&&!payload.accessToken)delete payload.accessToken;emit('submit',payload)}
</script>

<template>
  <form
    class="form-card"
    @submit.prevent="submit"
  >
    <div class="form-grid">
      <label class="field"><span>站点名称</span><input
        v-model.trim="form.name"
        required
        maxlength="80"
        autocomplete="off"
      ></label>
      <label class="field span-2"><span>站点地址</span><input
        v-model.trim="form.baseUrl"
        type="url"
        required
        maxlength="2048"
        placeholder="https://example.com"
      ></label>
      <label class="field"><span>站点类型</span><select v-model="form.adapter"><option
        v-if="!editing"
        value="auto"
      >自动识别</option><option
        v-for="adapter in adapters"
        :key="adapter.name"
        :value="adapter.name"
      >{{ adapter.displayName }}</option></select></label>
      <label class="field"><span>用户 ID</span><input
        v-model.trim="form.userId"
        maxlength="128"
        :required="selected?.capabilities.requiresUserId"
      ><small>仅 New API 必填</small></label>
      <label class="field span-2"><span>访问令牌</span><input
        v-model="form.accessToken"
        type="password"
        maxlength="4096"
        :required="!editing"
        autocomplete="new-password"
      ><small v-if="editing">留空表示保持现有令牌</small></label>
      <label class="field"><span>签到计划</span><input
        v-model.trim="form.checkinCron"
        required
        maxlength="100"
      ></label>
      <label class="field"><span>公告计划</span><input
        v-model.trim="form.announcementCron"
        required
        maxlength="100"
      ></label>
      <label class="field"><span>IANA 时区</span><input
        v-model.trim="form.timezone"
        required
        maxlength="100"
      ></label>
    </div>
    <fieldset class="switch-group">
      <legend>运行能力</legend><label><input
        v-model="form.enabled"
        type="checkbox"
      >启用站点</label><label><input
        v-model="form.checkinEnabled"
        type="checkbox"
        :disabled="form.adapter === 'auto' || (selected ? !selected.capabilities.checkin : false)"
      >定时签到</label><label><input
        v-model="form.announcementEnabled"
        type="checkbox"
        :disabled="form.adapter === 'auto' || (selected ? !selected.capabilities.announcements : false)"
      >公告同步</label>
    </fieldset>
    <div class="form-actions">
      <button
        class="button ghost"
        type="button"
        :disabled="submitting"
        @click="$emit('cancel')"
      >
        取消
      </button><button
        class="button primary"
        type="submit"
        :disabled="submitting"
      >
        {{ submitting ? '正在保存…' : editing ? '保存更改' : '添加站点' }}
      </button>
    </div>
  </form>
</template>
