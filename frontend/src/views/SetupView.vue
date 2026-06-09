<template>
  <h1 class="page-title">设置向导</h1>

  <div class="section-head">
    <div class="muted">按首次部署顺序检查关键配置状态</div>
    <el-button :loading="loading" @click="loadReadiness">
      <el-icon><Refresh /></el-icon>
      刷新
    </el-button>
  </div>

  <div class="panel setup-overview">
    <div>
      <span class="muted">完成度</span>
      <strong>{{ readiness?.completed || 0 }} / {{ readiness?.total || 0 }}</strong>
      <div class="muted">最后检查：{{ formatDateTimeUtc8(readiness?.generatedAt) }}</div>
    </div>
    <el-progress
      class="setup-progress"
      :percentage="readiness?.completionPct || 0"
      :status="readiness?.completionPct === 100 ? 'success' : undefined"
    />
  </div>

  <div v-loading="loading" class="setup-step-grid">
    <div v-for="step in steps" :key="step.key" class="setup-step-card">
      <div class="setup-step-card__head">
        <div>
          <h2>{{ step.title }}</h2>
          <div class="muted">{{ categoryLabel(step.category) }}</div>
        </div>
        <el-tag :type="stepTagType(step)" effect="plain">{{ stepStatusLabel(step) }}</el-tag>
      </div>
      <p>{{ step.summary }}</p>
      <div class="muted setup-step-card__detail">{{ step.detail }}</div>
      <div class="setup-step-card__actions">
        <el-button
          v-if="step.actionKey"
          type="primary"
          plain
          :loading="Boolean(running[step.actionKey])"
          @click="runStepAction(step)"
        >
          <el-icon><Operation /></el-icon>
          执行动作
        </el-button>
        <el-button v-if="step.route" plain @click="router.push(step.route)">
          <el-icon><Right /></el-icon>
          进入页面
        </el-button>
      </div>
    </div>
    <el-empty v-if="!steps.length && !loading" description="暂无设置状态" />
  </div>
</template>

<script setup lang="ts">
import { ElMessage } from 'element-plus'
import { Operation, Refresh, Right } from '@element-plus/icons-vue'
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, apiErrorText, unwrapJsonApiResource, type SetupReadiness, type SetupStep } from '../api'
import { formatDateTimeUtc8 } from '../utils/datetime'

const router = useRouter()
const readiness = ref<SetupReadiness | null>(null)
const loading = ref(false)
const running = reactive<Record<string, boolean>>({})

const steps = computed(() => readiness.value?.steps || [])

async function loadReadiness() {
  loading.value = true
  try {
    const { data } = await api.get('/setup/readiness')
    readiness.value = unwrapJsonApiResource<SetupReadiness>(data)
  } catch (error) {
    ElMessage.error(apiErrorText(error, '设置状态加载失败'))
  } finally {
    loading.value = false
  }
}

async function runStepAction(step: SetupStep) {
  if (!step.actionKey) return
  running[step.actionKey] = true
  try {
    const { data } = await api.post(`/setup/actions/${step.actionKey}`)
    const result = unwrapJsonApiResource<{ status: string; summary: string; detail?: string }>(data)
    if (result?.status === 'warning') {
      ElMessage.warning(result.detail || result.summary)
    } else {
      ElMessage.success(result?.summary || '动作已执行')
    }
    await loadReadiness()
  } catch (error) {
    ElMessage.error(apiErrorText(error, '设置动作执行失败'))
  } finally {
    running[step.actionKey] = false
  }
}

function stepTagType(step: SetupStep) {
  if (step.ready) return 'success'
  if (step.status === 'error') return 'danger'
  if (step.status === 'warning') return 'warning'
  return 'info'
}

function stepStatusLabel(step: SetupStep) {
  if (step.ready) return '已完成'
  if (step.optional) return '可选'
  const map: Record<string, string> = {
    warning: '待确认',
    missing: '待配置',
    error: '异常'
  }
  return map[step.status] || step.status
}

function categoryLabel(value: string) {
  const map: Record<string, string> = {
    access: '访问控制',
    research: '投研配置',
    trading: '模拟交易',
    data: '数据接入',
    runtime: '运行时'
  }
  return map[value] || value
}

onMounted(loadReadiness)
</script>

<style scoped>
.setup-overview {
  display: grid;
  grid-template-columns: minmax(180px, 280px) minmax(0, 1fr);
  gap: 24px;
  align-items: center;
}

.setup-overview strong {
  display: block;
  margin: 6px 0;
  font-size: 28px;
}

.setup-progress {
  width: 100%;
}

.setup-step-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 14px;
}

.setup-step-card {
  min-width: 0;
  border: 1px solid var(--border-soft);
  border-radius: 8px;
  padding: 16px;
  background: var(--surface);
  box-shadow: var(--shadow-card);
}

.setup-step-card__head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: flex-start;
}

.setup-step-card h2 {
  margin: 0 0 4px;
  font-size: 17px;
}

.setup-step-card p {
  margin: 14px 0 8px;
  line-height: 1.5;
}

.setup-step-card__detail {
  min-height: 44px;
  line-height: 1.5;
}

.setup-step-card__actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 14px;
}

@media (max-width: 767px) {
  .setup-overview {
    grid-template-columns: 1fr;
  }

  .setup-step-card__actions .el-button {
    flex: 1 1 calc(50% - 5px);
    margin-left: 0;
  }
}
</style>
