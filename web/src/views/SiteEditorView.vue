<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { api, APIError } from '@/api';
import PageState from '@/components/PageState.vue';
import SiteForm from '@/components/SiteForm.vue';
import type { AdapterDescriptor, Site, SiteWrite } from '@/types';

const route=useRoute();const router=useRouter();const id=computed(()=>typeof route.params.id==='string'?route.params.id:'');const editing=computed(()=>Boolean(id.value));const site=ref<Site>();const adapters=ref<AdapterDescriptor[]>([]);const loading=ref(true);const loadError=ref('');const submitting=ref(false);const submitError=ref('');let controller:AbortController|undefined;let requestSequence=0;
async function load(){controller?.abort();const activeController=new AbortController();controller=activeController;const sequence=++requestSequence;loading.value=true;loadError.value='';try{const [list,current]=await Promise.all([api.adapters(activeController.signal),editing.value?api.site(id.value,activeController.signal):Promise.resolve(undefined)]);if(sequence!==requestSequence)return;adapters.value=list;site.value=current}catch(caught){if(activeController.signal.aborted||sequence!==requestSequence)return;loadError.value=caught instanceof APIError?caught.message:'加载失败'}finally{if(sequence===requestSequence)loading.value=false}}
async function save(input:SiteWrite){if(submitting.value)return;submitting.value=true;submitError.value='';try{if(editing.value)await api.updateSite(id.value,input);else await api.createSite(input);await router.push('/sites')}catch(caught){submitError.value=caught instanceof APIError?caught.message:'保存失败'}finally{submitting.value=false}}
onMounted(load);
watch(id, load);
onBeforeUnmount(()=>controller?.abort());
</script>

<template>
  <div class="page narrow-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">
          {{ editing ? 'Edit site' : 'New site' }}
        </p><h1>{{ editing ? `编辑 ${site?.name ?? '站点'}` : '添加站点' }}</h1><p>能力开关必须与站点类型兼容；自动识别仅在创建时可用。</p>
      </div>
    </header><PageState
      :loading="loading"
      :error="loadError"
      @retry="load"
    >
      <template #content>
        <p
          v-if="submitError"
          class="form-error banner"
          role="alert"
        >
          {{ submitError }}
        </p><SiteForm
          :site="site"
          :adapters="adapters"
          :submitting="submitting"
          @submit="save"
          @cancel="router.push('/sites')"
        />
      </template>
    </PageState>
  </div>
</template>
