<script setup lang="ts">
import { ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { api, APIError } from '@/api';
import { setSession } from '@/session';

const token = ref(''); const submitting = ref(false); const error = ref('');
const route = useRoute(); const router = useRouter();
async function connect(){if(submitting.value)return;const candidate=token.value;submitting.value=true;error.value='';try{await api.validateToken(candidate);setSession(candidate);const redirect=typeof route.query.redirect==='string'?route.query.redirect:'/';await router.replace(redirect)}catch(caught){error.value=caught instanceof APIError?caught.message:'连接失败，请重试'}finally{submitting.value=false}}
</script>

<template>
  <section class="connect-page">
    <div class="connect-copy">
      <span class="brand-mark large">A</span><p class="eyebrow">
        Self-hosted companion
      </p><h1>把每天的签到与公告，收进一个安静的工作台。</h1><p>连接你的 APIHub 服务器。管理员令牌只保存在当前标签页，关闭标签页后自动消失。</p><ul><li>服务端定时签到，不依赖手机后台</li><li>New API、Sub2API、ZenAPI 能力化管理</li><li>默认公网 HTTPS 与受控上游访问</li></ul>
    </div>
    <form
      class="connect-card"
      @submit.prevent="connect"
    >
      <div>
        <p class="eyebrow">
          管理员连接
        </p><h2>进入 APIHub</h2><p class="muted">
          使用服务器配置的 ADMIN_TOKEN。
        </p>
      </div><label class="field"><span>管理员令牌</span><input
        v-model="token"
        type="password"
        required
        minlength="16"
        :disabled="submitting"
        autocomplete="current-password"
        autofocus
      ></label><p
        v-if="error"
        class="form-error"
        role="alert"
      >
        {{ error }}
      </p><button
        class="button primary wide-button"
        type="submit"
        :disabled="submitting"
      >
        {{ submitting ? '正在验证…' : '连接服务器' }}
      </button><small class="privacy-note">令牌存储位置：sessionStorage（当前标签页）</small>
    </form>
  </section>
</template>
