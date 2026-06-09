<template>
  <div class="login">
    <el-form class="login-panel" :model="form" label-position="top" @submit.prevent="submit">
      <div class="login-brand">
        <span class="brand-mark">投</span>
        <div>
          <h1>TradingCopilot</h1>
          <p>A 股协同投研</p>
        </div>
      </div>
      <el-alert v-if="bootstrapRequired" type="info" :closable="false" title="首次启动，请创建管理员账户。" />
      <el-form-item label="用户名"><el-input v-model="form.username" autocomplete="username" /></el-form-item>
      <el-form-item label="密码"><el-input v-model="form.password" type="password" autocomplete="current-password" /></el-form-item>
      <el-button class="login-submit" type="primary" native-type="submit" :loading="loading">进入控制台</el-button>
    </el-form>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { api, apiErrorText } from '../api'
import { setAuthToken } from '../auth'

const router = useRouter()
const route = useRoute()
const loading = ref(false)
const bootstrapRequired = ref(false)
const form = reactive({ username: 'admin', password: '' })

onMounted(async () => {
  const { data } = await api.get('/auth/bootstrap-required')
  bootstrapRequired.value = Boolean(data.data?.attributes?.bootstrapRequired)
})

async function submit() {
  loading.value = true
  try {
    const endpoint = bootstrapRequired.value ? '/auth/bootstrap' : '/auth/login'
    const { data } = await api.post(endpoint, {
      data: {
        type: bootstrapRequired.value ? 'auth-bootstrap-requests' : 'auth-login-requests',
        attributes: { username: form.username, password: form.password }
      }
    })
    setAuthToken(data.data.attributes.accessToken)
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/'
    router.push(redirect)
  } catch (error: any) {
    const message =
      error.response?.status === 401
        ? '用户名或密码不正确'
        : apiErrorText(error, '登录失败')
    ElMessage.error(message)
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: calc(24px + env(safe-area-inset-top, 0px)) 16px calc(24px + env(safe-area-inset-bottom, 0px));
  background: linear-gradient(180deg, #f7fafc 0%, var(--app-bg) 100%);
}

.login-panel {
  display: grid;
  gap: 16px;
  width: min(420px, 100%);
  background: var(--surface);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-panel);
  padding: 28px;
  box-shadow: 0 18px 42px rgba(22, 32, 51, 0.1);
}

.login-brand {
  display: flex;
  align-items: center;
  gap: 12px;
}

.login-brand h1 {
  margin: 0;
  color: var(--text-main);
  font-size: 24px;
  line-height: 1.2;
}

.login-brand p {
  margin: 3px 0 0;
  color: var(--text-muted);
  font-size: 13px;
}

.login-submit {
  width: 100%;
}

@media (max-width: 767px) {
  .login-panel {
    padding: 20px;
  }
}
</style>
