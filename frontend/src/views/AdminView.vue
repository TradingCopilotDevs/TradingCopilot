<template>
  <h1 class="page-title">团队安全</h1>

  <div class="section-head">
    <div class="muted">用户角色、会话撤销与敏感操作审计</div>
    <div class="toolbar compact-toolbar">
      <el-button :loading="loading" @click="loadAll">刷新</el-button>
      <el-button type="primary" @click="openCreateUser">新增用户</el-button>
    </div>
  </div>

  <el-tabs v-model="activeTab" class="panel">
    <el-tab-pane label="用户" name="users">
      <div v-if="isMobile" v-loading="loading" class="mobile-card-list">
        <div v-for="user in users" :key="user.id" class="mobile-card">
          <div class="mobile-card__header">
            <div>
              <h3 class="mobile-card__title">{{ user.displayName || user.username }}</h3>
              <div class="muted">{{ user.username }}</div>
            </div>
            <el-tag :type="user.active ? 'success' : 'info'" effect="plain">{{ user.active ? '启用' : '停用' }}</el-tag>
          </div>
          <div class="mobile-card__meta">
            <div class="mobile-card__meta-row">
              <span class="mobile-card__meta-label">角色</span>
              <span>{{ roleLabel(user.role) }}</span>
            </div>
            <div class="mobile-card__meta-row">
              <span class="mobile-card__meta-label">登录</span>
              <span>{{ formatDateTimeUtc8(user.lastLoginAt) }}</span>
            </div>
          </div>
          <div class="mobile-card__actions">
            <el-button plain @click="openEditUser(user)">编辑</el-button>
            <el-button plain @click="openResetPassword(user)">重置密码</el-button>
          </div>
        </div>
        <el-empty v-if="!users.length" description="暂无用户" />
      </div>

      <div v-else class="table-scroll">
        <el-table v-loading="loading" :data="users" empty-text="暂无用户">
          <el-table-column label="用户" min-width="180">
            <template #default="{ row }">
              <strong>{{ row.displayName || row.username }}</strong>
              <div class="muted">{{ row.username }}</div>
            </template>
          </el-table-column>
          <el-table-column label="角色" width="120">
            <template #default="{ row }">{{ roleLabel(row.role) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <el-tag :type="row.active ? 'success' : 'info'" effect="plain">{{ row.active ? '启用' : '停用' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="最近登录" min-width="180">
            <template #default="{ row }">{{ formatDateTimeUtc8(row.lastLoginAt) }}</template>
          </el-table-column>
          <el-table-column label="创建时间" min-width="180">
            <template #default="{ row }">{{ formatDateTimeUtc8(row.createdAt) }}</template>
          </el-table-column>
          <el-table-column label="操作" width="180" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" @click="openEditUser(row)">编辑</el-button>
              <el-button link type="primary" @click="openResetPassword(row)">重置密码</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </el-tab-pane>

    <el-tab-pane label="会话" name="sessions">
      <div class="toolbar compact-toolbar-stack">
        <el-input v-model="sessionUsername" clearable placeholder="按用户名过滤" style="max-width: 220px" @keyup.enter="loadSessions" />
        <el-checkbox v-model="includeRevoked" @change="loadSessions">显示已撤销</el-checkbox>
        <el-button :loading="sessionsLoading" @click="loadSessions">查询</el-button>
      </div>

      <div class="table-scroll">
        <el-table v-loading="sessionsLoading" :data="sessions" empty-text="暂无会话">
          <el-table-column prop="username" label="用户" width="140" />
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <el-tag :type="sessionTagType(row.status)" effect="plain">{{ sessionStatusLabel(row.status) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="过期时间" min-width="180">
            <template #default="{ row }">{{ formatDateTimeUtc8(row.expiresAt) }}</template>
          </el-table-column>
          <el-table-column prop="ip" label="IP" min-width="160" />
          <el-table-column prop="userAgent" label="User Agent" min-width="260" show-overflow-tooltip />
          <el-table-column label="撤销信息" min-width="220">
            <template #default="{ row }">
              <span v-if="row.revokedAt">{{ formatDateTimeUtc8(row.revokedAt) }} / {{ row.revokedBy || '-' }}</span>
              <span v-else>-</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="100" fixed="right">
            <template #default="{ row }">
              <el-button v-if="row.status === 'active'" link type="danger" @click="revokeSession(row)">撤销</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </el-tab-pane>

    <el-tab-pane label="审计" name="audit">
      <div class="table-scroll">
        <el-table v-loading="auditLoading" :data="auditEvents" empty-text="暂无审计事件">
          <el-table-column label="时间" min-width="180">
            <template #default="{ row }">{{ formatDateTimeUtc8(row.createdAt) }}</template>
          </el-table-column>
          <el-table-column prop="actor" label="操作者" width="140" />
          <el-table-column prop="action" label="动作" min-width="190" />
          <el-table-column label="资源" min-width="180">
            <template #default="{ row }">{{ row.resourceType }} / {{ row.resourceId || '-' }}</template>
          </el-table-column>
          <el-table-column prop="outcome" label="结果" width="100" />
          <el-table-column prop="detail" label="详情" min-width="260" show-overflow-tooltip />
        </el-table>
      </div>
    </el-tab-pane>
  </el-tabs>

  <el-dialog v-model="createDialogVisible" title="新增用户" :width="dialogWidth" :fullscreen="isMobile">
    <el-form :model="createForm" :label-width="isMobile ? 'auto' : '96px'" :label-position="isMobile ? 'top' : 'right'">
      <el-form-item label="用户名">
        <el-input v-model="createForm.username" />
      </el-form-item>
      <el-form-item label="显示名">
        <el-input v-model="createForm.displayName" />
      </el-form-item>
      <el-form-item label="角色">
        <el-select v-model="createForm.role">
          <el-option v-for="role in roles" :key="role.value" :label="role.label" :value="role.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="初始密码">
        <el-input v-model="createForm.password" type="password" show-password />
      </el-form-item>
      <el-form-item label="启用">
        <el-switch v-model="createForm.active" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="createDialogVisible = false">取消</el-button>
      <el-button type="primary" :loading="saving" @click="createUser">保存</el-button>
    </template>
  </el-dialog>

  <el-dialog v-model="editDialogVisible" title="编辑用户" :width="dialogWidth" :fullscreen="isMobile">
    <el-form :model="editForm" :label-width="isMobile ? 'auto' : '96px'" :label-position="isMobile ? 'top' : 'right'">
      <el-form-item label="显示名">
        <el-input v-model="editForm.displayName" />
      </el-form-item>
      <el-form-item label="角色">
        <el-select v-model="editForm.role">
          <el-option v-for="role in roles" :key="role.value" :label="role.label" :value="role.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="启用">
        <el-switch v-model="editForm.active" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="editDialogVisible = false">取消</el-button>
      <el-button type="primary" :loading="saving" @click="updateUser">保存</el-button>
    </template>
  </el-dialog>

  <el-dialog v-model="resetDialogVisible" title="重置密码" :width="dialogWidth" :fullscreen="isMobile">
    <el-alert type="warning" :closable="false" title="重置后原密码立即失效。该操作会写入审计事件。" />
    <el-form :model="resetForm" class="dialog-form" :label-width="isMobile ? 'auto' : '96px'" :label-position="isMobile ? 'top' : 'right'">
      <el-form-item label="新密码">
        <el-input v-model="resetForm.password" type="password" show-password />
      </el-form-item>
      <el-form-item label="二次确认">
        <el-checkbox v-model="resetForm.confirm">确认重置该用户密码</el-checkbox>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="resetDialogVisible = false">取消</el-button>
      <el-button type="danger" :loading="saving" @click="resetPassword">确认重置</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, onMounted, reactive, ref } from 'vue'
import {
  api,
  apiErrorText,
  jsonapiResource,
  unwrapJsonApiCollection,
  type AdminUser,
  type AuditEvent,
  type AuthSession
} from '../api'
import { useResponsive } from '../composables/useResponsive'
import { formatDateTimeUtc8 } from '../utils/datetime'

const { isMobile } = useResponsive()
const activeTab = ref('users')
const users = ref<AdminUser[]>([])
const sessions = ref<AuthSession[]>([])
const auditEvents = ref<AuditEvent[]>([])
const loading = ref(false)
const sessionsLoading = ref(false)
const auditLoading = ref(false)
const saving = ref(false)
const includeRevoked = ref(false)
const sessionUsername = ref('')
const selectedUser = ref<AdminUser | null>(null)
const createDialogVisible = ref(false)
const editDialogVisible = ref(false)
const resetDialogVisible = ref(false)
const dialogWidth = computed(() => (isMobile.value ? '100%' : '520px'))

const roles = [
  { label: '所有者', value: 'owner' },
  { label: '管理员', value: 'admin' },
  { label: '操作员', value: 'operator' },
  { label: '只读', value: 'viewer' }
]

const createForm = reactive({
  username: '',
  displayName: '',
  role: 'admin' as AdminUser['role'],
  password: '',
  active: true
})

const editForm = reactive({
  displayName: '',
  role: 'admin' as AdminUser['role'],
  active: true
})

const resetForm = reactive({
  password: '',
  confirm: false
})

async function loadAll() {
  loading.value = true
  try {
    await Promise.all([loadUsers(), loadSessions(), loadAuditEvents()])
  } finally {
    loading.value = false
  }
}

async function loadUsers() {
  const { data } = await api.get('/admin/users')
  users.value = unwrapJsonApiCollection<AdminUser>(data)
}

async function loadSessions() {
  sessionsLoading.value = true
  try {
    const { data } = await api.get('/admin/sessions', {
      params: {
        username: sessionUsername.value || undefined,
        includeRevoked: includeRevoked.value || undefined,
        'page[limit]': 200
      }
    })
    sessions.value = unwrapJsonApiCollection<AuthSession>(data)
  } finally {
    sessionsLoading.value = false
  }
}

async function loadAuditEvents() {
  auditLoading.value = true
  try {
    const { data } = await api.get('/audit-events', { params: { 'page[limit]': 200 } })
    auditEvents.value = unwrapJsonApiCollection<AuditEvent>(data)
  } finally {
    auditLoading.value = false
  }
}

function openCreateUser() {
  Object.assign(createForm, { username: '', displayName: '', role: 'admin', password: '', active: true })
  createDialogVisible.value = true
}

function openEditUser(user: AdminUser) {
  selectedUser.value = user
  Object.assign(editForm, { displayName: user.displayName, role: user.role, active: user.active })
  editDialogVisible.value = true
}

function openResetPassword(user: AdminUser) {
  selectedUser.value = user
  Object.assign(resetForm, { password: '', confirm: false })
  resetDialogVisible.value = true
}

async function createUser() {
  saving.value = true
  try {
    await api.post('/admin/users', jsonapiResource('admin-users', createForm))
    ElMessage.success('用户已创建')
    createDialogVisible.value = false
    await loadAll()
  } catch (error) {
    ElMessage.error(apiErrorText(error, '用户创建失败'))
  } finally {
    saving.value = false
  }
}

async function updateUser() {
  if (!selectedUser.value) return
  saving.value = true
  try {
    await api.put(`/admin/users/${selectedUser.value.id}`, jsonapiResource('admin-users', editForm, String(selectedUser.value.id)))
    ElMessage.success('用户已更新')
    editDialogVisible.value = false
    await loadAll()
  } catch (error) {
    ElMessage.error(apiErrorText(error, '用户更新失败'))
  } finally {
    saving.value = false
  }
}

async function resetPassword() {
  if (!selectedUser.value) return
  saving.value = true
  try {
    await api.post(
      `/admin/users/${selectedUser.value.id}/password-reset`,
      jsonapiResource('admin-user-password-resets', resetForm)
    )
    ElMessage.success('密码已重置')
    resetDialogVisible.value = false
    await loadAll()
  } catch (error) {
    ElMessage.error(apiErrorText(error, '密码重置失败'))
  } finally {
    saving.value = false
  }
}

async function revokeSession(session: AuthSession) {
  try {
    await ElMessageBox.confirm('撤销后该 access token 将无法继续访问系统。', '确认撤销会话', {
      type: 'warning',
      confirmButtonText: '撤销',
      cancelButtonText: '取消'
    })
    await api.post(
      `/admin/sessions/${session.id}/revoke`,
      jsonapiResource('auth-session-revokes', { reason: 'manual revoke from console', confirm: true })
    )
    ElMessage.success('会话已撤销')
    await Promise.all([loadSessions(), loadAuditEvents()])
  } catch (error) {
    if (error === 'cancel') return
    ElMessage.error(apiErrorText(error, '会话撤销失败'))
  }
}

function roleLabel(value: string) {
  return roles.find((role) => role.value === value)?.label || value || '-'
}

function sessionStatusLabel(value: string) {
  const map: Record<string, string> = {
    active: '活跃',
    revoked: '已撤销',
    expired: '已过期'
  }
  return map[value] || value || '-'
}

function sessionTagType(value: string) {
  const map: Record<string, 'success' | 'warning' | 'info'> = {
    active: 'success',
    revoked: 'warning',
    expired: 'info'
  }
  return map[value] || 'info'
}

onMounted(loadAll)
</script>
