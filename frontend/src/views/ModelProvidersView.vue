<template>
  <h1 class="page-title">模型提供商</h1>

  <div class="panel">
    <div class="section-head">
      <h2>模型接入</h2>
      <el-button type="primary" @click="openProvider()">新增服务商</el-button>
    </div>

    <div v-if="isMobile" class="mobile-card-list">
      <div v-for="provider in providers" :key="provider.id" class="mobile-card">
        <div class="mobile-card__header">
          <div>
            <h3 class="mobile-card__title">{{ provider.name }}</h3>
            <div class="muted">{{ provider.baseUrl }}</div>
          </div>
          <el-tag :type="provider.enabled ? 'success' : 'info'">{{ provider.enabled ? '启用' : '停用' }}</el-tag>
        </div>
        <div class="mobile-card__meta">
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">默认模型</span>
            <span>{{ provider.defaultModel || '-' }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">接口密钥</span>
            <span>{{ provider.hasApiKey ? '已配置' : '未配置' }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">已同步模型</span>
            <span>{{ providerModels[provider.id]?.length || 0 }}</span>
          </div>
        </div>
        <div class="mobile-card__actions">
          <el-button plain @click="openProvider(provider)">编辑</el-button>
          <el-button type="primary" plain @click="syncModels(provider)">同步模型</el-button>
        </div>
      </div>
      <el-empty v-if="!providers.length" description="暂无服务商" />
    </div>

    <el-table v-else :data="providers" empty-text="暂无服务商">
      <el-table-column prop="name" label="名称" width="160" />
      <el-table-column prop="baseUrl" label="接口地址" />
      <el-table-column prop="defaultModel" label="默认模型" width="240" />
      <el-table-column label="接口密钥" width="110">
        <template #default="{ row }">
          <el-tag :type="row.hasApiKey ? 'success' : 'info'">{{ row.hasApiKey ? '已配置' : '未配置' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="已同步模型" width="120">
        <template #default="{ row }">{{ providerModels[row.id]?.length || 0 }}</template>
      </el-table-column>
      <el-table-column label="启用" width="90">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '启用' : '停用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="180">
        <template #default="{ row }">
          <el-button link type="primary" @click="openProvider(row)">编辑</el-button>
          <el-button link type="primary" @click="syncModels(row)">同步模型</el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>

  <el-dialog v-model="providerDialog" title="模型服务商" :fullscreen="isMobile" :width="providerDialogWidth">
    <el-alert
      type="info"
      :closable="false"
      title="接口地址和接口密钥是同一个服务商的成组配置。保存服务商后，可从上游模型列表同步可用模型。编辑时接口密钥留空表示不修改原密钥。"
    />
    <el-form
      class="dialog-form"
      :model="providerForm"
      :label-width="isMobile ? 'auto' : '130px'"
      :label-position="isMobile ? 'top' : 'right'"
    >
      <el-form-item label="名称"><el-input v-model="providerForm.name" /></el-form-item>
      <el-form-item label="接口地址"><el-input v-model="providerForm.baseUrl" /></el-form-item>
      <el-form-item label="接口密钥">
        <el-input
          v-model="providerForm.apiKey"
          type="password"
          show-password
          :placeholder="editingProviderId ? '留空表示不修改原接口密钥' : '输入该服务商的接口密钥'"
        />
      </el-form-item>
      <el-form-item label="默认模型">
        <el-select
          v-if="editingProviderId && providerModels[editingProviderId]?.length"
          v-model="providerForm.defaultModel"
          filterable
          placeholder="选择默认模型"
        >
          <el-option
            v-for="model in providerModels[editingProviderId]"
            :key="model.modelId"
            :label="model.displayName"
            :value="model.modelId"
          />
        </el-select>
        <el-input v-else v-model="providerForm.defaultModel" placeholder="保存并同步模型后可下拉选择" />
      </el-form-item>
      <el-form-item label="启用"><el-switch v-model="providerForm.enabled" /></el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="providerDialog = false">取消</el-button>
      <el-button v-if="editingProviderId" :loading="syncing" @click="syncCurrentProvider">从上游同步模型</el-button>
      <el-button type="primary" @click="saveProvider">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ElMessage } from 'element-plus'
import { computed, onMounted, reactive, ref } from 'vue'
import { api, apiErrorText, jsonapiResource, unwrapJsonApiCollection } from '../api'
import { useResponsive } from '../composables/useResponsive'

const { isMobile, isTablet } = useResponsive()
const providers = ref<any[]>([])
const providerModels = reactive<Record<number, any[]>>({})
const providerDialog = ref(false)
const syncing = ref(false)
const editingProviderId = ref<number | null>(null)

const providerDialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '90vw' : '760px'))

const providerForm = reactive<any>({
  name: '',
  baseUrl: 'https://api.openai.com/v1',
  defaultModel: '',
  apiKey: '',
  enabled: true
})

async function loadProviderModels(providerId: number) {
  providerModels[providerId] = unwrapJsonApiCollection((await api.get(`/ai/providers/${providerId}/models`)).data)
}

async function load() {
  providers.value = unwrapJsonApiCollection((await api.get('/ai/providers')).data)
  await Promise.all(providers.value.map((provider) => loadProviderModels(provider.id)))
}

function openProvider(provider?: any) {
  editingProviderId.value = provider?.id ?? null
  Object.assign(providerForm, {
    name: provider?.name ?? '',
    baseUrl: provider?.baseUrl ?? 'https://api.openai.com/v1',
    defaultModel: provider?.defaultModel ?? '',
    apiKey: '',
    enabled: provider?.enabled ?? true
  })
  providerDialog.value = true
}

async function saveProvider() {
  try {
    const payload = {
      ...providerForm,
      apiKey: providerForm.apiKey || null
    }
    if (editingProviderId.value) {
      await api.put(`/ai/providers/${editingProviderId.value}`, jsonapiResource('ai-providers', payload, String(editingProviderId.value)))
    } else {
      const { data } = await api.post('/ai/providers', jsonapiResource('ai-providers', payload))
      editingProviderId.value = Number(data?.data?.id)
    }
    providerDialog.value = false
    ElMessage.success('服务商已保存')
    await load()
  } catch (error: unknown) {
    ElMessage.error(apiErrorText(error, '服务商保存失败'))
  }
}

async function syncModels(provider: any) {
  syncing.value = true
  try {
    const { data } = await api.post(`/ai/providers/${provider.id}/models/sync`)
    providerModels[provider.id] = unwrapJsonApiCollection(data)
    ElMessage.success(`已同步 ${providerModels[provider.id].length} 个模型`)
  } finally {
    syncing.value = false
  }
}

async function syncCurrentProvider() {
  const provider = providers.value.find((item) => item.id === editingProviderId.value)
  if (provider) await syncModels(provider)
}

onMounted(load)
</script>
