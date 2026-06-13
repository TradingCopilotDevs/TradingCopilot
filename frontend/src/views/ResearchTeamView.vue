<template>
  <h1 class="page-title">投研团队</h1>

  <div class="panel">
    <div class="section-head">
      <h2>团队</h2>
      <div class="toolbar compact-toolbar">
        <el-button type="primary" @click="openTeam()">新建团队</el-button>
        <el-button :loading="loading" @click="load">刷新</el-button>
      </div>
    </div>
    <div v-if="isMobile" v-loading="loading" class="mobile-card-list">
      <div
        v-for="team in teams"
        :key="team.id"
        class="mobile-card selectable-card"
        :class="{ 'selectable-card--selected': selectedTeam?.id === team.id }"
        @click="selectTeam(team)"
      >
        <div class="mobile-card__header">
          <div>
            <h3 class="mobile-card__title">{{ team.name }}</h3>
            <div class="muted">{{ team.description || '-' }}</div>
          </div>
          <el-tag v-if="selectedTeam?.id === team.id" type="primary" effect="dark">当前</el-tag>
          <el-tag v-else :type="team.active ? 'success' : 'info'">{{ team.active ? '启用' : '停用' }}</el-tag>
        </div>
        <div class="mobile-card__meta">
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">资产类型</span>
            <span>{{ assetClassLabel(team.assetClass) }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">执行账户</span>
            <span>{{ executionAccountLabel(team) }}</span>
          </div>
        </div>
        <div class="mobile-card__actions" @click.stop>
          <el-button plain @click="openTeam(team)">编辑</el-button>
          <el-button type="primary" plain @click="selectTeam(team)">角色</el-button>
          <el-popconfirm title="删除团队会删除其会议、关注项和唤醒计划" @confirm="deleteTeam(team)">
            <template #reference><el-button plain type="danger">删除</el-button></template>
          </el-popconfirm>
        </div>
      </div>
      <el-empty v-if="!teams.length" description="暂无团队" />
    </div>

    <el-table
      v-else
      v-loading="loading"
      :data="teams"
      row-key="id"
      highlight-current-row
      :current-row-key="selectedTeam?.id"
      :row-class-name="teamRowClassName"
      empty-text="暂无团队"
      class="research-team-table"
      @row-click="selectTeam"
    >
      <el-table-column width="52" align="center">
        <template #default="{ row }">
          <el-tag v-if="selectedTeam?.id === row.id" type="primary" size="small" effect="dark">当前</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="name" label="名称" min-width="180" />
      <el-table-column prop="description" label="说明" min-width="220" />
      <el-table-column label="资产类型" width="140">
        <template #default="{ row }">{{ assetClassLabel(row.assetClass) }}</template>
      </el-table-column>
      <el-table-column label="执行账户" width="180">
        <template #default="{ row }">{{ executionAccountLabel(row) }}</template>
      </el-table-column>
      <el-table-column label="状态" width="90">
        <template #default="{ row }">
          <el-tag :type="row.active ? 'success' : 'info'">{{ row.active ? '启用' : '停用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="210">
        <template #default="{ row }">
          <el-button link type="primary" @click.stop="openTeam(row)">编辑</el-button>
          <el-button link type="primary" @click.stop="selectTeam(row)">角色</el-button>
          <el-popconfirm title="删除团队会删除其会议、关注项和唤醒计划" @confirm="deleteTeam(row)">
            <template #reference><el-button link type="danger" @click.stop>删除</el-button></template>
          </el-popconfirm>
        </template>
      </el-table-column>
    </el-table>
  </div>

  <div class="panel">
    <div class="section-head">
      <h2>{{ selectedTeam ? `${selectedTeam.name} 角色` : '角色' }}</h2>
      <div class="toolbar compact-toolbar">
        <el-button :disabled="!selectedTeam" :loading="running.applyDefaults" @click="applyDefaults">应用默认角色</el-button>
        <el-button type="primary" :disabled="!selectedTeam" @click="openRole()">新增角色</el-button>
      </div>
    </div>

    <div v-if="isMobile" v-loading="rolesLoading" class="mobile-card-list">
      <div v-for="role in roles" :key="role.key" class="mobile-card">
        <div class="mobile-card__header">
          <div>
            <h3 class="mobile-card__title">{{ role.sortOrder }}. {{ role.name }}</h3>
            <div class="muted">{{ role.key }}</div>
          </div>
          <el-tag :type="role.enabled ? 'success' : 'info'">{{ role.enabled ? '启用' : '停用' }}</el-tag>
        </div>
        <div class="mobile-card__meta">
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">模型</span>
            <span>{{ providerName(role.providerId) || '-' }} / {{ role.model || '默认' }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">职责</span>
            <span>{{ role.responsibility || '-' }}</span>
          </div>
        </div>
        <div class="mobile-card__actions">
          <el-button plain @click="openRole(role)">编辑</el-button>
          <el-popconfirm title="删除该团队角色？" @confirm="deleteRole(role)">
            <template #reference><el-button plain type="danger">删除</el-button></template>
          </el-popconfirm>
        </div>
      </div>
      <el-empty v-if="!roles.length" :description="selectedTeam ? '暂无角色' : '请选择团队'" />
    </div>

    <el-table v-else v-loading="rolesLoading" :data="roles" empty-text="请选择团队">
      <el-table-column prop="sortOrder" label="顺序" width="80" />
      <el-table-column prop="key" label="Key" width="150" />
      <el-table-column prop="name" label="名称" width="160" />
      <el-table-column prop="responsibility" label="职责" min-width="240" />
      <el-table-column label="模型" width="220">
        <template #default="{ row }">{{ providerName(row.providerId) || '-' }} / {{ row.model || '默认' }}</template>
      </el-table-column>
      <el-table-column label="状态" width="90">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '启用' : '停用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="140">
        <template #default="{ row }">
          <el-button link type="primary" @click="openRole(row)">编辑</el-button>
          <el-popconfirm title="删除该团队角色？" @confirm="deleteRole(row)">
            <template #reference><el-button link type="danger">删除</el-button></template>
          </el-popconfirm>
        </template>
      </el-table-column>
    </el-table>
  </div>

  <el-dialog v-model="teamDialog" title="投研团队" :width="teamDialogWidth" :fullscreen="isMobile">
    <el-form :model="teamForm" :label-width="isMobile ? 'auto' : '120px'" :label-position="isMobile ? 'top' : 'right'">
      <el-form-item label="名称"><el-input v-model="teamForm.name" /></el-form-item>
      <el-form-item label="说明"><el-input v-model="teamForm.description" type="textarea" :rows="3" /></el-form-item>
      <el-form-item label="资产类型">
        <el-select v-model="teamForm.assetClass" style="width: 100%">
          <el-option label="A 股" value="a_share" />
          <el-option label="预测市场" value="prediction_market" />
          <el-option label="混合" value="mixed" />
        </el-select>
      </el-form-item>
      <el-form-item v-if="teamForm.assetClass !== 'prediction_market'" label="模拟盘账户">
        <el-select v-model="teamForm.paperAccountId" filterable style="width: 100%">
          <el-option v-for="account in selectableAccounts" :key="account.id" :label="account.name" :value="account.id" />
        </el-select>
      </el-form-item>
      <el-form-item v-if="!teamForm.id" label="复制角色">
        <el-select v-model="teamForm.copyRolesFromTeamId" clearable style="width: 100%">
          <el-option v-for="team in teams" :key="team.id" :label="team.name" :value="team.id" />
        </el-select>
      </el-form-item>
      <el-form-item label="启用"><el-switch v-model="teamForm.active" /></el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="teamDialog = false">取消</el-button>
      <el-button type="primary" :loading="running.saveTeam" @click="saveTeam">保存</el-button>
    </template>
  </el-dialog>

  <el-dialog v-model="roleDialog" title="团队角色" :width="roleDialogWidth" :fullscreen="isMobile">
    <el-form :model="roleForm" :label-width="isMobile ? 'auto' : '120px'" :label-position="isMobile ? 'top' : 'right'">
      <el-form-item label="Key"><el-input v-model="roleForm.key" :disabled="Boolean(roleForm.originalKey)" /></el-form-item>
      <el-form-item label="名称"><el-input v-model="roleForm.name" /></el-form-item>
      <el-form-item label="启用"><el-switch v-model="roleForm.enabled" /></el-form-item>
      <el-form-item label="顺序"><el-input-number v-model="roleForm.sortOrder" :min="1" :max="999" /></el-form-item>
      <el-form-item label="服务商">
        <el-select v-model="roleForm.providerId" clearable style="width: 100%" @change="roleForm.model = ''">
          <el-option v-for="provider in providers" :key="provider.id" :label="provider.name" :value="provider.id" />
        </el-select>
      </el-form-item>
      <el-form-item label="模型">
        <el-select
          v-if="roleForm.providerId && providerModels[roleForm.providerId]?.length"
          v-model="roleForm.model"
          clearable
          filterable
          placeholder="留空使用服务商默认模型"
          style="width: 100%"
        >
          <el-option
            v-for="model in providerModels[roleForm.providerId]"
            :key="model.modelId"
            :label="model.displayName"
            :value="model.modelId"
          />
        </el-select>
        <el-input v-else v-model="roleForm.model" placeholder="该服务商尚未同步模型，可手动填写" />
      </el-form-item>
      <el-form-item label="职责"><el-input v-model="roleForm.responsibility" type="textarea" :rows="3" /></el-form-item>
      <el-form-item label="提示词"><el-input v-model="roleForm.promptTemplate" type="textarea" :rows="8" /></el-form-item>
      <el-form-item label="工具"><el-select v-model="roleForm.toolNames" multiple filterable style="width: 100%"><el-option v-for="tool in roleTools" :key="tool.key" :label="`${tool.title} / ${tool.key}`" :value="tool.key" /></el-select></el-form-item>
      <el-form-item label="技能"><el-select v-model="roleForm.skillNames" multiple filterable style="width: 100%"><el-option v-for="skill in roleSkills" :key="skill.key" :label="`${skill.title} / ${skill.key}`" :value="skill.key" /></el-select></el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="roleDialog = false">取消</el-button>
      <el-button type="primary" :loading="running.saveRole" @click="saveRole">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ElMessageBox } from 'element-plus'
import { computed, onMounted, reactive, ref } from 'vue'
import { api, jsonapiResource, unwrapJsonApiCollection } from '../api'
import { useAsyncAction } from '../composables/useAsyncAction'
import { useResponsive } from '../composables/useResponsive'

const teams = ref<any[]>([])
const accounts = ref<any[]>([])
const roles = ref<any[]>([])
const providers = ref<any[]>([])
const providerModels = reactive<Record<number, any[]>>({})
const tools = ref<any[]>([])
const skills = ref<any[]>([])
const selectedTeam = ref<any | null>(null)
const teamDialog = ref(false)
const roleDialog = ref(false)
const loading = ref(false)
const rolesLoading = ref(false)
const { isMobile, isTablet } = useResponsive()
const { running, runAction } = useAsyncAction()

const teamForm = reactive<any>({ id: null, name: '', description: '', paperAccountId: null, assetClass: 'a_share', copyRolesFromTeamId: null, active: true })
const roleForm = reactive<any>({ originalKey: '', key: '', name: '', responsibility: '', promptTemplate: '', providerId: null, model: '', toolNames: [], skillNames: [], enabled: true, sortOrder: 100 })

const selectableAccounts = computed(() => accounts.value.filter((account) => !account.researchTeamId || account.id === teamForm.paperAccountId))
const teamDialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '90vw' : '640px'))
const roleDialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '92vw' : '900px'))
const selectedTeamIsPrediction = computed(() => selectedTeam.value?.assetClass === 'prediction_market')
const predictionAllowedSkillKeys = new Set([
  'news-source-verification',
  'web-research',
  'meeting-moderation',
  'cross-examination',
  'evidence-synthesis',
  'prediction-market-research',
  'odds-market-analysis',
  'market-resolution-risk',
  'wake-plan-design'
])
const roleTools = computed(() => (selectedTeamIsPrediction.value ? tools.value.filter((tool) => isPredictionRoleToolAllowed(tool.key)) : tools.value))
const roleSkills = computed(() => (selectedTeamIsPrediction.value ? skills.value.filter((skill) => isPredictionRoleSkillAllowed(skill.key)) : skills.value))

function accountName(id: number) {
  if (!id) return '-'
  return accounts.value.find((account) => account.id === id)?.name || `#${id}`
}

function executionAccountLabel(team: any) {
  if (team?.assetClass === 'prediction_market') return '不绑定'
  return accountName(team?.paperAccountId)
}

function assetClassLabel(value?: string) {
  return ({ a_share: 'A 股', prediction_market: '预测市场', mixed: '混合' } as Record<string, string>)[value || 'a_share'] || 'A 股'
}

function providerName(id?: number | null) {
  return providers.value.find((provider) => provider.id === id)?.name
}

function teamRowClassName({ row }: { row: any }) {
  return selectedTeam.value?.id === row.id ? 'is-selected-team' : ''
}

async function load() {
  loading.value = true
  try {
    const [teamResp, accountResp, providerResp, toolResp, skillResp] = await Promise.all([
      api.get('/research-teams'),
      api.get('/paper/accounts'),
      api.get('/ai/providers'),
      api.get('/ai/tool-definitions'),
      api.get('/ai/skill-definitions')
    ])
    teams.value = unwrapJsonApiCollection(teamResp.data)
    accounts.value = unwrapJsonApiCollection(accountResp.data)
    providers.value = unwrapJsonApiCollection(providerResp.data)
    tools.value = unwrapJsonApiCollection(toolResp.data)
    skills.value = unwrapJsonApiCollection(skillResp.data)
    await Promise.all(providers.value.map((provider) => loadProviderModels(provider.id)))
    if (selectedTeam.value) {
      selectedTeam.value = teams.value.find((team) => team.id === selectedTeam.value.id) || null
      await loadRoles()
    }
  } finally {
    loading.value = false
  }
}

async function loadProviderModels(providerId: number) {
  providerModels[providerId] = unwrapJsonApiCollection((await api.get(`/ai/providers/${providerId}/models`)).data)
}

async function selectTeam(team: any) {
  selectedTeam.value = team
  await loadRoles()
}

async function loadRoles() {
  if (!selectedTeam.value) {
    roles.value = []
    return
  }
  rolesLoading.value = true
  try {
    roles.value = unwrapJsonApiCollection((await api.get(`/research-teams/${selectedTeam.value.id}/roles`)).data)
  } finally {
    rolesLoading.value = false
  }
}

function openTeam(team?: any) {
  Object.assign(teamForm, team ? { ...team, assetClass: team.assetClass || 'a_share', copyRolesFromTeamId: null } : { id: null, name: '', description: '', paperAccountId: null, assetClass: 'a_share', copyRolesFromTeamId: null, active: true })
  teamDialog.value = true
}

async function saveTeam() {
  await runAction('saveTeam', async () => {
    const attrs = {
      ...teamForm,
      paperAccountId: teamForm.assetClass === 'prediction_market' ? null : Number(teamForm.paperAccountId || 0),
      copyRolesFromTeamId: teamForm.copyRolesFromTeamId || null
    }
    if (teamForm.id) await api.put(`/research-teams/${teamForm.id}`, jsonapiResource('research-teams', attrs, String(teamForm.id)))
    else await api.post('/research-teams', jsonapiResource('research-teams', attrs))
    teamDialog.value = false
    await load()
  }, { success: '团队已保存' })
}

async function deleteTeam(team: any) {
  await runAction(`deleteTeam:${team.id}`, async () => {
    await api.delete(`/research-teams/${team.id}`)
    if (selectedTeam.value?.id === team.id) selectedTeam.value = null
    await load()
  }, { success: '团队已删除' })
}

function openRole(role?: any) {
  Object.assign(roleForm, role ? { ...role, originalKey: role.key, model: role.model || '', toolNames: role.toolNames || [], skillNames: role.skillNames || [] } : { originalKey: '', key: '', name: '', responsibility: '', promptTemplate: '', providerId: null, model: '', toolNames: [], skillNames: [], enabled: true, sortOrder: 100 })
  sanitizeRoleFormCapabilities()
  roleDialog.value = true
}

async function saveRole() {
  if (!selectedTeam.value) return
  sanitizeRoleFormCapabilities()
  await runAction('saveRole', async () => {
    const key = roleForm.originalKey || roleForm.key
    await api.put(`/research-teams/${selectedTeam.value.id}/roles/${key}`, jsonapiResource('research-team-roles', { ...roleForm, model: roleForm.model || null, providerId: roleForm.providerId || null }, `${selectedTeam.value.id}:${key}`))
    roleDialog.value = false
    await loadRoles()
  }, { success: '角色已保存' })
}

async function deleteRole(role: any) {
  if (!selectedTeam.value) return
  await runAction(`deleteRole:${role.key}`, async () => {
    await api.delete(`/research-teams/${selectedTeam.value.id}/roles/${role.key}`)
    await loadRoles()
  }, { success: '角色已删除' })
}

async function applyDefaults() {
  if (!selectedTeam.value) return
  try {
    await ElMessageBox.confirm(
      '这会删除当前团队所有角色，并按初始化时默认团队所有人员配置重建。',
      '重置默认角色',
      { type: 'warning', confirmButtonText: '重置', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  await runAction('applyDefaults', async () => {
    await api.post(`/research-teams/${selectedTeam.value.id}/roles/apply-defaults`)
    await loadRoles()
  }, { success: '默认角色已应用' })
}

function sanitizeRoleFormCapabilities() {
  if (!selectedTeamIsPrediction.value) return
  roleForm.toolNames = filterRoleValues(roleForm.toolNames, isPredictionRoleToolAllowed)
  roleForm.skillNames = filterRoleValues(roleForm.skillNames, isPredictionRoleSkillAllowed)
}

function filterRoleValues(values: string[] | null | undefined, allowed: (value: string) => boolean) {
  const seen = new Set<string>()
  return (values || []).reduce<string[]>((out, raw) => {
    const value = String(raw || '').trim()
    if (!value || !allowed(value) || seen.has(value)) return out
    seen.add(value)
    out.push(value)
    return out
  }, [])
}

function isPredictionRoleToolAllowed(key?: string) {
  const value = String(key || '').trim()
  return value === 'web.search' || value.startsWith('meeting.') || value.startsWith('prediction.')
}

function isPredictionRoleSkillAllowed(key?: string) {
  return predictionAllowedSkillKeys.has(String(key || '').trim())
}

onMounted(load)
</script>

<style scoped>
:deep(.research-team-table .el-table__row) {
  cursor: pointer;
}

:deep(.research-team-table .el-table__row.is-selected-team > .el-table__cell) {
  background: var(--el-color-primary-light-9);
}

:deep(.research-team-table .el-table__row.is-selected-team:hover > .el-table__cell) {
  background: var(--el-color-primary-light-8);
}

:deep(.research-team-table .el-table__row.is-selected-team .el-table__cell:first-child) {
  box-shadow: inset 3px 0 0 var(--el-color-primary);
}
</style>
