<template>
  <h1 class="page-title">平台适配器</h1>

  <div class="panel">
    <div class="section-head">
      <h2>Telegram Bot 发送适配器</h2>
      <el-button :loading="loading" @click="load">刷新</el-button>
    </div>
    <el-form class="dialog-form" :model="draft" :label-width="isMobile ? 'auto' : '110px'" :label-position="isMobile ? 'top' : 'right'">
      <el-form-item label="名称">
        <el-input v-model="draft.displayName" />
      </el-form-item>
      <el-form-item label="Bot Token">
        <el-input v-model="draft.botToken" type="password" show-password />
      </el-form-item>
      <el-form-item label="Chat ID">
        <el-input v-model="draft.chatId" placeholder="-1001234567890" />
      </el-form-item>
      <el-form-item label="启用">
        <el-switch v-model="draft.enabled" />
      </el-form-item>
      <el-button type="primary" :loading="running.createAdapter" @click="createAdapter">新增适配器</el-button>
    </el-form>
  </div>

  <div class="panel">
    <div v-if="isMobile" v-loading="loading" class="mobile-card-list">
      <div v-for="adapter in adapters" :key="adapter.id" class="mobile-card">
        <div class="mobile-card__header">
          <div>
            <h3 class="mobile-card__title">{{ adapter.displayName }}</h3>
            <div class="muted">{{ adapter.provider }}</div>
          </div>
          <el-tag :type="adapter.enabled ? 'success' : 'info'">{{ adapter.enabled ? '启用' : '停用' }}</el-tag>
        </div>
        <div class="mobile-card__meta">
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">Token</span>
            <span>{{ adapter.hasBotToken ? '已配置' : '未配置' }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">Chat ID</span>
            <span>{{ adapter.chatId || '-' }}</span>
          </div>
        </div>
        <div class="mobile-card__actions">
          <el-button plain :loading="running[`testAdapter:${adapter.id}`]" @click="testAdapter(adapter)">发送测试</el-button>
          <el-button plain type="primary" :loading="running[`toggleAdapter:${adapter.id}`]" @click="toggle(adapter)">
            {{ adapter.enabled ? '停用' : '启用' }}
          </el-button>
          <el-popconfirm title="删除该平台适配器？" @confirm="deleteAdapter(adapter)">
            <template #reference><el-button plain type="danger">删除</el-button></template>
          </el-popconfirm>
        </div>
      </div>
      <el-empty v-if="!adapters.length" description="暂无适配器" />
    </div>

    <el-table v-else v-loading="loading" :data="adapters" empty-text="暂无适配器">
      <el-table-column prop="displayName" label="名称" min-width="160" />
      <el-table-column prop="provider" label="Provider" width="160" />
      <el-table-column label="状态" width="220">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '启用' : '停用' }}</el-tag>
          <el-tag class="tag-gap" :type="row.hasBotToken ? 'success' : 'warning'">Token</el-tag>
          <el-tag class="tag-gap" :type="row.hasChatId ? 'success' : 'warning'">{{ row.chatId || 'Chat ID' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="240">
        <template #default="{ row }">
          <el-button link type="primary" :loading="running[`testAdapter:${row.id}`]" @click="testAdapter(row)">发送测试</el-button>
          <el-button link type="primary" :loading="running[`toggleAdapter:${row.id}`]" @click="toggle(row)">{{ row.enabled ? '停用' : '启用' }}</el-button>
          <el-popconfirm title="删除该平台适配器？" @confirm="deleteAdapter(row)">
            <template #reference>
              <el-button link type="danger">删除</el-button>
            </template>
          </el-popconfirm>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup lang="ts">
import { ElMessage } from 'element-plus'
import { onMounted, reactive, ref } from 'vue'
import { api, jsonapiResource, unwrapJsonApiCollection, type PlatformAdapter } from '../api'
import { useAsyncAction } from '../composables/useAsyncAction'
import { useResponsive } from '../composables/useResponsive'

const adapters = ref<PlatformAdapter[]>([])
const draft = reactive({ provider: 'telegram_bot', displayName: 'Telegram Bot', botToken: '', chatId: '', enabled: true })
const loading = ref(false)
const { isMobile } = useResponsive()
const { running, runAction } = useAsyncAction()

async function load() {
  loading.value = true
  try {
    const { data } = await api.get('/platform-adapters')
    adapters.value = unwrapJsonApiCollection(data)
  } finally {
    loading.value = false
  }
}

async function createAdapter() {
  if (!draft.botToken || !draft.chatId) {
    ElMessage.warning('Bot Token 和 Chat ID 都必须填写')
    return
  }
  await runAction('createAdapter', async () => {
    await api.post('/platform-adapters', jsonapiResource('platform-adapters', draft))
    Object.assign(draft, { provider: 'telegram_bot', displayName: 'Telegram Bot', botToken: '', chatId: '', enabled: true })
    await load()
  }, { success: '适配器已新增' })
}

async function toggle(row: PlatformAdapter) {
  await runAction(`toggleAdapter:${row.id}`, async () => {
    await api.put(`/platform-adapters/${row.id}`, jsonapiResource('platform-adapters', { enabled: !row.enabled }, String(row.id)))
    await load()
  }, { success: row.enabled ? '适配器已停用' : '适配器已启用' })
}

async function deleteAdapter(row: PlatformAdapter) {
  await runAction(`deleteAdapter:${row.id}`, async () => {
    await api.delete(`/platform-adapters/${row.id}`)
    await load()
  }, { success: '适配器已删除' })
}

async function testAdapter(row: PlatformAdapter) {
  await runAction(`testAdapter:${row.id}`, async () => {
    await api.post(`/platform-adapters/${row.id}/test`)
  }, { success: '测试消息已发送' })
}

onMounted(load)
</script>
