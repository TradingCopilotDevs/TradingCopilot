<template>
  <div class="section-head">
    <h1 class="page-title">会议 #{{ id }}</h1>
    <div class="toolbar compact-toolbar">
      <el-button @click="router.push('/meetings')">返回列表</el-button>
      <el-button v-if="meeting?.status === 'completed'" @click="rerunRecap">重新总结</el-button>
      <el-button v-if="canRestart" type="primary" plain @click="restartMeeting">
        {{ canCancel ? '取消并重新开会' : '重新开会' }}
      </el-button>
      <el-button @click="openReferenceDialog">引用会议</el-button>
      <el-button v-if="canCancel" type="danger" plain @click="cancelMeeting">取消会议</el-button>
    </div>
  </div>

  <div class="panel">
    <div class="meeting-head">
      <div>
        <strong>{{ meeting?.topic }}</strong>
        <p class="muted">{{ triggerSourceLabel(meeting?.triggerSource) }} / {{ formatDateTimeUtc8(meeting?.createdAt) }}</p>
      </div>
      <el-tag :type="statusType">{{ meetingStatusLabel(meeting?.status) }}</el-tag>
    </div>
    <div class="toolbar compact-toolbar">
      <el-tag v-for="tag in meeting?.tags || []" :key="tag" class="tag-gap">{{ tag }}</el-tag>
      <span v-if="!(meeting?.tags || []).length" class="muted">暂无标签</span>
    </div>
    <el-progress :percentage="progressPercent" :status="meeting?.status === 'failed' ? 'exception' : undefined" />
    <div class="meeting-stats">
      <span>当前角色：{{ currentRole || '-' }}</span>
      <span>模型调用：{{ modelCallCount }} 次</span>
      <span>完成角色：{{ completedRoleCount }} 个</span>
      <span>错误：{{ errorCount }} 个</span>
      <span>重总结状态：{{ recapStatusLabel(meeting?.recapStatus) }}</span>
    </div>
    <div v-if="roleStates.length" class="role-progress-line">
      <el-tag v-for="role in roleStates" :key="role.name" :type="roleTagType(role.status)">
        {{ role.name }} / {{ role.statusText }}
      </el-tag>
    </div>
  </div>

  <div class="detail-grid">
    <div class="panel chat-panel">
      <BubbleList :list="chatItems" :auto-scroll="true" :show-back-button="true" :max-height="chatMaxHeight" item-key="id">
        <template #avatar="{ item }">
          <div class="chat-avatar" :class="`chat-avatar-${item.kind}`">{{ item.initial }}</div>
        </template>
        <template #header="{ item }">
          <div class="chat-header">
            <strong>{{ item.title }}</strong>
            <el-tag size="small" :type="item.tagType">{{ item.status }}</el-tag>
          </div>
        </template>
        <template #content="{ item }">
          <div class="markdown-body" v-html="item.html"></div>
        </template>
        <template #footer="{ item }">
          <div class="chat-footer">{{ item.time }}</div>
        </template>
      </BubbleList>
      <div class="cursor-pagination-footer">
        <span class="muted">已加载 {{ events.length }} 条记录</span>
        <el-button v-if="eventNextCursor" plain :loading="eventsLoadingMore" @click="loadOlderEvents">加载更早记录</el-button>
      </div>
      <el-empty v-if="!chatItems.length" description="暂无会议记录" />
    </div>

    <div class="detail-side">
      <div class="panel detail-side-panel">
        <el-tabs v-model="detailSideTab" class="detail-side-tabs">
          <el-tab-pane label="可信度" name="trust">
            <div class="section-head">
              <h2>可信度</h2>
              <el-tag :type="confidenceTagType(trustReport?.confidence)" effect="plain">
                {{ confidenceLabel(trustReport?.confidence) }}
              </el-tag>
            </div>
        <div class="trust-metrics">
          <div>
            <span>证据</span>
            <strong>{{ trustReport?.evidenceCount ?? 0 }}</strong>
          </div>
          <div>
            <span>引用</span>
            <strong>{{ trustReport?.citationCount ?? 0 }}</strong>
          </div>
          <div>
            <span>模型快照</span>
            <strong>{{ trustReport?.modelSnapshotCount ?? 0 }}</strong>
          </div>
          <div>
            <span>结论版本</span>
            <strong>{{ trustConclusionCount }}</strong>
          </div>
        </div>
        <div class="trust-gate" :class="`trust-gate-${trustEvidenceGateStatus}`">
          <div>
            <strong>证据门禁</strong>
            <span>{{ trustEvidenceGateSummary }}</span>
          </div>
          <el-tag :type="evidenceGateTagType(trustEvidenceGateStatus)" effect="dark">
            {{ evidenceGateLabel(trustEvidenceGateStatus) }}
          </el-tag>
        </div>
        <div v-if="trustUnsupportedClaims.length" class="trust-section">
          <strong>缺证声明</strong>
          <div class="trust-binding-list">
            <div v-for="item in trustUnsupportedClaims" :key="item.id || item.claim" class="trust-binding-item trust-unsupported-item">
              <el-tag size="small" :type="claimTypeTag(item.claimType)" effect="plain">{{ claimTypeLabel(item.claimType) }}</el-tag>
              <span class="trust-binding-claim">{{ item.claim }}</span>
              <span class="muted">{{ unsupportedClaimText(item) }}</span>
            </div>
          </div>
        </div>
        <div v-if="trustEvidenceItems.length" class="trust-section">
          <strong>结构化证据</strong>
          <div class="trust-evidence-list">
            <div v-for="item in trustEvidenceItems" :key="trustEvidenceKey(item)" class="trust-evidence-item">
              <div class="trust-evidence-head">
                <el-tag size="small" :type="trustEvidenceTagType(item.status)" effect="plain">
                  {{ trustEvidenceStatusLabel(item.status) }}
                </el-tag>
                <span class="muted">{{ trustEvidenceTitle(item) }}</span>
              </div>
              <div class="trust-evidence-preview">{{ trustEvidencePreviewText(item) }}</div>
              <div v-if="trustEvidenceMarketId(item)" class="toolbar compact-toolbar">
                <el-button link type="primary" @click="openPredictionMarketEvidence(item)">
                  打开预测市场
                </el-button>
              </div>
            </div>
          </div>
        </div>
        <div v-if="trustRecapActionSuggestions.length" class="trust-section">
          <strong>待复核动作</strong>
          <div class="trust-action-list">
            <div v-for="item in trustRecapActionSuggestions" :key="item.id || `${item.eventId}-${item.actionIndex}`" class="trust-action-item">
              <div class="trust-action-head">
                <el-tag size="small" :type="recapActionTagType(item.disposition)" effect="plain">
                  {{ recapActionTypeLabel(item.actionType) }}
                </el-tag>
                <span class="muted">{{ recapActionDispositionLabel(item.disposition) }} / 事件 #{{ item.eventId || '-' }}</span>
              </div>
              <div class="trust-action-spec">{{ recapActionSpecText(item.spec, item.actionType) }}</div>
              <div class="muted trust-action-reason">{{ item.reason || '需要人工复核后再执行' }}</div>
              <div class="muted trust-action-evidence">{{ recapActionEvidenceText(item.evidenceSummary) }}</div>
              <div v-if="recapActionLatestReview(item)" class="trust-action-review">
                <el-tag size="small" :type="recapActionReviewTag(recapActionLatestReview(item)?.decision)" effect="plain">
                  {{ recapActionReviewLabel(recapActionLatestReview(item)?.decision) }}
                </el-tag>
                <span class="muted">{{ recapActionReviewMeta(recapActionLatestReview(item)) }}</span>
              </div>
              <el-input
                v-model="recapActionReviewComments[recapActionKey(item)]"
                size="small"
                placeholder="动作复核备注"
                clearable
              />
              <div class="toolbar compact-toolbar">
                <el-button size="small" type="success" plain :loading="recapActionReviewLoading" @click="submitRecapActionReview(item, 'approved')">
                  确认有效
                </el-button>
                <el-button size="small" type="warning" plain :loading="recapActionReviewLoading" @click="submitRecapActionReview(item, 'needs_evidence')">
                  补证据
                </el-button>
                <el-button size="small" type="danger" plain :loading="recapActionReviewLoading" @click="submitRecapActionReview(item, 'rejected')">
                  驳回
                </el-button>
              </div>
            </div>
          </div>
        </div>
        <div class="trust-section">
          <strong>事实 / 假设 / 推断</strong>
          <div class="trust-claims">
            <el-tag v-for="item in trustFacts" :key="`fact-${item}`" type="success" effect="plain">{{ item }}</el-tag>
            <el-tag v-for="item in trustAssumptions" :key="`assumption-${item}`" type="warning" effect="plain">{{ item }}</el-tag>
            <el-tag v-for="item in trustInferences" :key="`inference-${item}`" type="info" effect="plain">{{ item }}</el-tag>
            <span v-if="!trustFacts.length && !trustAssumptions.length && !trustInferences.length" class="muted">暂无结构化声明</span>
          </div>
        </div>
        <div v-if="trustClaimBindings.length" class="trust-section">
          <strong>声明证据绑定</strong>
          <div class="trust-binding-list">
            <div v-for="item in trustClaimBindings" :key="item.id" class="trust-binding-item">
              <el-tag size="small" :type="claimTypeTag(item.claimType)" effect="plain">{{ claimTypeLabel(item.claimType) }}</el-tag>
              <span class="trust-binding-claim">{{ item.claim }}</span>
              <span class="muted">{{ bindingEvidenceText(item) }}</span>
            </div>
          </div>
        </div>
        <div v-if="trustEvidenceGaps.length" class="trust-section">
          <strong>证据缺口</strong>
          <ul class="trust-gap-list">
            <li v-for="item in trustEvidenceGaps" :key="`evidence-gap-${item}`">{{ item }}</li>
          </ul>
        </div>
        <div v-if="trustConclusionDiffs.length" class="trust-section">
          <strong>结论变更</strong>
          <div class="trust-diff-list">
            <div v-for="(item, index) in trustConclusionDiffs" :key="`diff-${index}`" class="trust-diff-item">
              <el-tag :type="item.changed ? 'warning' : 'info'" effect="plain">{{ item.changed ? '已变化' : '未变化' }}</el-tag>
              <span>{{ trustDiffText(item) }}</span>
            </div>
          </div>
        </div>
        <div v-if="trustReviewCandidates.length" class="trust-section">
          <strong>结论逐句复核</strong>
          <div class="trust-review-list">
            <div v-for="item in trustReviewCandidates" :key="item.key" class="trust-review-item">
              <div class="trust-review-sentence">{{ item.sentence }}</div>
              <div class="trust-review-meta">
                <el-tag
                  v-if="item.latestReview"
                  size="small"
                  :type="reviewVerdictTag(item.latestReview.verdict)"
                  effect="plain"
                >
                  {{ reviewVerdictLabel(item.latestReview.verdict) }}
                </el-tag>
                <span v-else class="muted">未复核</span>
                <span class="muted">{{ trustReviewRefsText(item) }}</span>
              </div>
              <el-input
                v-model="trustReviewComments[item.key]"
                size="small"
                placeholder="复核备注"
                clearable
              />
              <div class="toolbar compact-toolbar">
                <el-button size="small" type="success" plain :loading="trustReviewLoading" @click="submitTrustReview(item, 'confirmed')">
                  确认
                </el-button>
                <el-button size="small" type="warning" plain :loading="trustReviewLoading" @click="submitTrustReview(item, 'needs_evidence')">
                  补证据
                </el-button>
                <el-button size="small" type="danger" plain :loading="trustReviewLoading" @click="submitTrustReview(item, 'rejected')">
                  驳回
                </el-button>
              </div>
            </div>
          </div>
        </div>
        <div v-if="trustGaps.length" class="trust-section">
          <strong>缺口</strong>
          <ul class="trust-gap-list">
            <li v-for="item in trustGaps" :key="item">{{ trustGapLabel(item) }}</li>
          </ul>
        </div>
          </el-tab-pane>

          <el-tab-pane label="引用链" name="references">
            <div class="section-head">
              <h2>引用链</h2>
              <el-button link type="primary" @click="loadReferences">刷新</el-button>
            </div>
        <div v-if="!referenceChain.length" class="muted">暂无引用</div>
        <div
          v-for="(item, index) in referenceChain"
          :key="`${item.id}-${item.depth}`"
          class="reference-card"
          :class="{
            'reference-card-deleted': item.targetDeleted,
            'reference-card-leaf': isLeafReference(index),
            'reference-card-external': item.referenceType !== 'meeting'
          }"
          :style="{ marginLeft: referenceIndent(item.depth) }"
        >
          <div class="reference-path">
            <span class="reference-depth">第 {{ item.depth + 1 }} 层</span>
            <span class="reference-path-text">{{ referencePathText(item, index) }}</span>
          </div>
          <div class="reference-head">
            <strong>{{ item.targetTopicSnapshot }}</strong>
            <div class="toolbar compact-toolbar">
              <el-tag size="small" type="primary">{{ referenceTypeLabel(item.referenceType) }}</el-tag>
              <el-tag size="small" :type="referenceStatusType(item, index)">
                {{ referenceStatusLabel(item, index) }}
              </el-tag>
            </div>
          </div>
          <div class="muted">{{ item.note || '无备注' }}</div>
          <div v-if="item.targetSummarySnapshot" class="muted reference-summary">{{ item.targetSummarySnapshot }}</div>
          <div class="reference-foot">
            <span class="muted">{{ formatDateTimeUtc8(item.createdAt) }}</span>
            <span class="muted">{{ referenceHint(item, index) }}</span>
          </div>
          <div class="toolbar compact-toolbar">
            <el-button v-if="canOpenReference(item)" link type="primary" @click="router.push(`/meetings/${item.targetMeetingId}`)">
              打开会议
            </el-button>
            <span v-else class="muted">{{ closedReferenceLabel(item, index) }}</span>
          </div>
        </div>
          </el-tab-pane>

          <el-tab-pane label="唤醒计划" name="wake">
            <div class="section-head">
              <h2>唤醒计划</h2>
              <el-button link type="primary" @click="loadWakePlans">刷新</el-button>
            </div>
        <div v-if="!wakePlans.length" class="muted">暂无唤醒计划</div>
        <div v-for="plan in wakePlans" :key="plan.id" class="reference-card">
          <div class="reference-head">
            <strong>#{{ plan.id }} {{ wakeTriggerLabel(plan.triggerType) }}</strong>
            <div class="toolbar compact-toolbar">
              <el-tag size="small">{{ wakeStatusLabel(plan.status) }}</el-tag>
              <el-button link type="danger" @click="deleteWakePlan(plan)">删除</el-button>
            </div>
          </div>
          <div>{{ plan.reason }}</div>
          <div class="muted">下次检查：{{ formatDateTimeUtc8(plan.nextCheckAt) }}</div>
        </div>
          </el-tab-pane>
        </el-tabs>
      </div>
    </div>
  </div>

  <el-dialog
    v-model="referenceDialogVisible"
    title="引用历史会议"
    :fullscreen="isMobile"
    :width="dialogWidth"
  >
    <el-form
      :model="referenceForm"
      :label-width="isMobile ? 'auto' : '90px'"
      :label-position="isMobile ? 'top' : 'right'"
    >
      <el-form-item label="目标会议">
        <el-select v-model="referenceForm.targetMeetingId" filterable style="width: 100%">
          <el-option
            v-for="item in referenceCandidates"
            :key="item.id"
            :label="`#${item.id} ${item.topic}`"
            :value="item.id"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="备注">
        <el-input v-model="referenceForm.note" type="textarea" :rows="3" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="referenceDialogVisible = false">取消</el-button>
      <el-button type="primary" @click="saveReference">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import DOMPurify from 'dompurify'
import { ElMessage, ElMessageBox } from 'element-plus'
import MarkdownIt from 'markdown-it'
import { BubbleList } from 'vue-element-plus-x'
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  api,
  apiErrorText,
  jsonapiResource,
  unwrapJsonApiCollection,
  unwrapJsonApiResource,
  type Meeting,
  type MeetingEvent,
  type MeetingRecapActionReviewAttributes,
  type MeetingRecapActionSuggestion,
  type MeetingReference,
  type MeetingTrustReviewAttributes,
  type WakePlan
} from '../api'
import { useAutoRefresh } from '../composables/useAutoRefresh'
import { useResponsive } from '../composables/useResponsive'
import { formatDateTimeUtc8 } from '../utils/datetime'

interface ChatItem {
  id: number
  html: string
  initial: string
  kind: string
  placement: 'start'
  shape: 'corner'
  tagType: 'success' | 'danger' | 'warning' | 'info' | 'primary'
  status: string
  time: string
  title: string
  variant: 'filled' | 'outlined' | 'shadow'
}

interface RoleState {
  name: string
  status: string
  statusText: string
}

type ChainItem = MeetingReference & { depth: number }
type TrustReviewVerdict = 'confirmed' | 'needs_evidence' | 'rejected' | 'superseded'
type RecapActionReviewDecision = 'approved' | 'needs_evidence' | 'rejected' | 'superseded'

interface TrustReviewCandidate {
  key: string
  sentence: string
  sentenceId: string
  citationIds: string[]
  evidenceEventIds: number[]
  latestReview?: MeetingTrustReviewAttributes
}

const route = useRoute()
const router = useRouter()
const { isMobile, isTablet } = useResponsive()
const id = Number(route.params.id)
const meeting = ref<Meeting | null>(null)
const events = ref<MeetingEvent[]>([])
const eventNextCursor = ref('')
const eventsLoadingMore = ref(false)
const wakePlans = ref<WakePlan[]>([])
const referenceChain = ref<ChainItem[]>([])
const referenceCandidates = ref<Meeting[]>([])
const referenceDialogVisible = ref(false)
const referenceForm = reactive({ targetMeetingId: undefined as number | undefined, note: '' })
const detailSideTab = ref('trust')
const trustReviewComments = reactive<Record<string, string>>({})
const trustReviewLoading = ref(false)
const recapActionReviewComments = reactive<Record<string, string>>({})
const recapActionReviewLoading = ref(false)
const markdown = new MarkdownIt({ html: false, linkify: true, breaks: true })

const dialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '90vw' : '620px'))
const chatMaxHeight = computed(() => (isMobile.value ? 'calc(100vh - 300px)' : 'calc(100vh - 360px)'))

const canCancel = computed(() => meeting.value?.status === 'queued' || meeting.value?.status === 'running')
const canRestart = computed(() => Boolean(meeting.value))
const modelCallCount = computed(() => events.value.filter((event) => payloadStatus(event) === 'model_call').length)
const completedRoleCount = computed(() => events.value.filter((event) => payloadStatus(event) === 'role_completed').length)
const errorCount = computed(() => events.value.filter((event) => event.type === 'error').length)
const currentRole = computed(() => {
  if (meeting.value?.status === 'completed') return '已完成'
  if (meeting.value?.status === 'failed') return '失败'
  if (meeting.value?.status === 'cancelled') return '已取消'
  if (meeting.value?.recapStatus === 'running') return '主持人总结中'
  const activeEvent = [...events.value].reverse().find((event) =>
    ['role_started', 'model_call', 'moderator_kickoff', 'moderator_review', 'recap_completed'].includes(payloadStatus(event))
  )
  return payloadText(activeEvent, 'role_name') || activeEvent?.roleKey
})
const progressPercent = computed(() => {
  if (meeting.value?.status === 'completed') return 100

  const roleProgressEvents = events.value.filter((event) =>
    ['role_started', 'model_call', 'role_completed', 'role_error', 'skipped'].includes(payloadStatus(event))
  )
  const totalUnits = roleProgressEvents.reduce((max, event) => {
    const progress = meetingProgress(event)
    return progress && progress.total > max ? progress.total : max
  }, 0)
  const completedRoleUnits = roleProgressEvents.reduce((max, event) => {
    const progress = meetingProgress(event)
    return progress && progress.current > max ? progress.current : max
  }, 0)
  const kickoffDone = events.value.some((event) => payloadStatus(event) === 'moderator_kickoff') ? 1 : 0
  const reviewDone = events.value.filter((event) => payloadStatus(event) === 'moderator_review').length
  const recapDone = events.value.some((event) => payloadStatus(event) === 'recap_completed') ? 1 : 0

  if (totalUnits > 0) {
    const completedUnits = Math.min(totalUnits, completedRoleUnits + kickoffDone + reviewDone + recapDone)
    let percent = Math.round((completedUnits / totalUnits) * 100)
    if (meeting.value?.recapStatus === 'running' && recapDone === 0) {
      percent = Math.max(percent, 95)
    }
    if (meeting.value?.status === 'failed' || meeting.value?.status === 'cancelled') {
      return Math.max(1, Math.min(99, percent))
    }
    return Math.max(kickoffDone > 0 ? 5 : 1, Math.min(99, percent))
  }

  if (meeting.value?.recapStatus === 'running') return 95
  if (events.value.some((event) => payloadStatus(event) === 'moderator_kickoff')) return 5
  if (events.value.some((event) => payloadStatus(event) === 'running')) return 2
  return meeting.value?.status === 'queued' ? 0 : 1
})
const statusType = computed(() => {
  if (meeting.value?.status === 'completed') return 'success'
  if (meeting.value?.status === 'failed') return 'danger'
  if (meeting.value?.status === 'cancelled') return 'info'
  return 'warning'
})
const trustReport = computed(() => meeting.value?.trustReport || null)
const trustFacts = computed(() => trustReport.value?.claimBreakdown?.facts || [])
const trustAssumptions = computed(() => trustReport.value?.claimBreakdown?.assumptions || [])
const trustInferences = computed(() => trustReport.value?.claimBreakdown?.inferences || [])
const trustEvidenceGaps = computed(() => trustReport.value?.evidenceGaps || [])
const trustConclusionDiffs = computed<Record<string, any>[]>(() => trustReport.value?.conclusionDiffs || [])
const trustGaps = computed(() => trustReport.value?.gaps || [])
const trustConclusionCount = computed(() => trustReport.value?.conclusionHistory?.length || 0)
const trustEvidenceGate = computed<Record<string, any>>(() => {
  const gate = trustReport.value?.evidenceGate as Record<string, any> | undefined
  if (gate) return gate
  return {
    status: trustReport.value?.evidenceGateStatus || 'warning',
    unsupportedClaimCount: trustReport.value?.unsupportedClaimCount || 0,
    unsupportedClaims: trustReport.value?.unsupportedClaims || []
  }
})
const trustEvidenceGateStatus = computed(() => String(trustEvidenceGate.value.status || 'warning'))
const trustUnsupportedClaims = computed<Record<string, any>[]>(() => {
  const claims = trustEvidenceGate.value.unsupportedClaims || trustReport.value?.unsupportedClaims || []
  return Array.isArray(claims) ? claims.slice(0, 8) : []
})
const trustEvidenceGateSummary = computed(() => evidenceGateSummary(trustEvidenceGate.value))
const trustEvidenceItems = computed<Record<string, any>[]>(() => {
  const evidence = trustReport.value?.evidence || []
  return Array.isArray(evidence) ? evidence.slice(0, 8) : []
})
const trustClaimBindings = computed<Record<string, any>[]>(() => (trustReport.value?.claimEvidenceBindings || []).slice(0, 6))
const trustSentenceReviews = computed<MeetingTrustReviewAttributes[]>(() => trustReport.value?.sentenceReviews || [])
const trustRecapActionSuggestions = computed<MeetingRecapActionSuggestion[]>(() => {
  const suggestions = trustReport.value?.recapActionSuggestions || []
  return Array.isArray(suggestions) ? suggestions.slice(0, 8) : []
})
const trustReviewCandidates = computed<TrustReviewCandidate[]>(() => {
  const sentences = currentConclusionSentences().slice(0, 8)
  return sentences.map((sentence, index) => {
    const sentenceKey = normalizeTrustSentenceText(sentence)
    const key = `${sentenceKey || 'sentence'}:${index}`
    const binding = matchingTrustBinding(sentence)
    const latestReview = [...trustSentenceReviews.value].reverse().find((review) => normalizeTrustSentenceText(review.sentence || '') === sentenceKey)
    return {
      key,
      sentence,
      sentenceId: String(latestReview?.sentenceId || ''),
      citationIds: trustStringArray(binding?.citationIds),
      evidenceEventIds: trustNumberArray(binding?.evidenceEventIds),
      latestReview
    }
  })
})
const roleStates = computed<RoleState[]>(() => {
  const byName = new Map<string, RoleState>()
  for (const event of events.value) {
    const roleKey = event.roleKey || payloadText(event, 'role_name')
    if (!roleKey) continue
    const roleName = roleDisplayName(event)
    const status = payloadStatus(event)
    if (status === 'role_started') byName.set(roleKey, { name: roleName, status, statusText: '工作中' })
    if (status === 'model_call') byName.set(roleKey, { name: roleName, status, statusText: '调用模型' })
    if (status === 'role_completed') byName.set(roleKey, { name: roleName, status, statusText: '完成' })
    if (status === 'role_error') byName.set(roleKey, { name: roleName, status, statusText: '失败' })
  }
  return [...byName.values()]
})
const chatItems = computed<ChatItem[]>(() =>
  events.value.map((event) => {
    const kind = eventKind(event)
    return {
      id: event.id,
      html: renderMarkdown(event.content),
      initial: initialFor(event),
      kind,
      placement: 'start',
      shape: 'corner',
      tagType: eventTagType(event),
      status: eventStatusLabel(payloadStatus(event) || event.type),
      time: formatDateTimeUtc8(event.createdAt),
      title: eventTitle(event),
      variant: kind === 'conclusion' ? 'shadow' : kind === 'system' ? 'outlined' : 'filled'
    }
  })
)

async function loadMeeting() {
  meeting.value = unwrapJsonApiResource((await api.get(`/meetings/${id}`)).data)
}

async function fetchEventPage(cursor?: string) {
  const { data } = await api.get(`/meetings/${id}/events`, {
    params: { 'page[limit]': 100, 'page[cursor]': cursor || undefined }
  })
  return {
    rows: unwrapJsonApiCollection<MeetingEvent>(data),
    nextCursor: String(data?.meta?.nextCursor || '')
  }
}

async function loadEvents(background = false) {
  const page = await fetchEventPage()
  eventNextCursor.value = page.nextCursor
  events.value = background ? dedupeEvents(events.value.concat(page.rows)) : dedupeEvents(page.rows)
}

async function loadOlderEvents() {
  if (!eventNextCursor.value || eventsLoadingMore.value) return
  eventsLoadingMore.value = true
  try {
    const page = await fetchEventPage(eventNextCursor.value)
    eventNextCursor.value = page.nextCursor
    events.value = dedupeEvents(page.rows.concat(events.value))
  } finally {
    eventsLoadingMore.value = false
  }
}

async function loadWakePlans() {
  wakePlans.value = unwrapJsonApiCollection((await api.get('/wake-plans', { params: { meetingId: id } })).data)
}

async function deleteWakePlan(plan: WakePlan) {
  await ElMessageBox.confirm(`确定删除唤醒计划 #${plan.id} 吗？`, '删除唤醒计划', { type: 'warning' })
  await api.delete(`/wake-plans/${plan.id}`)
  ElMessage.success('唤醒计划已删除')
  await loadWakePlans()
}

async function buildReferenceChain(meetingId: number, depth = 0, seen = new Set<number>()) {
  if (seen.has(meetingId) || depth > 3) return []
  seen.add(meetingId)
  const { data } = await api.get(`/meetings/${meetingId}/references`)
  const references = unwrapJsonApiCollection<MeetingReference>(data)
  const items: ChainItem[] = []
  for (const item of references) {
    items.push({ ...item, depth })
    if (item.targetMeetingId && !item.targetDeleted) {
      items.push(...(await buildReferenceChain(item.targetMeetingId, depth + 1, seen)))
    }
  }
  return items
}

async function loadReferences() {
  referenceChain.value = await buildReferenceChain(id)
}

async function loadInitial() {
  await Promise.all([loadMeeting(), loadEvents()])
  await Promise.all([loadWakePlans(), loadReferences()])
}

async function cancelMeeting() {
  meeting.value = unwrapJsonApiResource((await api.post(`/meetings/${id}/cancel`)).data)
  ElMessage.success('会议已取消')
}

async function restartMeeting() {
  const { data } = await api.post(`/meetings/${id}/restart`)
  const restarted = unwrapJsonApiResource<Meeting>(data)
  if (!restarted) return
  ElMessage.success(`已重新开会，新的会议编号：#${restarted.id}`)
  await router.push(`/meetings/${restarted.id}`)
}

async function rerunRecap() {
  await api.post(`/meetings/${id}/recap`, jsonapiResource('meeting-recaps', { requestedBy: 'ui' }))
  ElMessage.success('已触发主持人重新总结')
  await loadMeeting()
}

async function openReferenceDialog() {
  referenceCandidates.value = unwrapJsonApiCollection<Meeting>((await api.get('/meetings', { params: { 'page[limit]': 200 } })).data).filter(
    (item: Meeting) => item.id !== id
  )
  referenceDialogVisible.value = true
}

async function saveReference() {
  if (!referenceForm.targetMeetingId) {
    ElMessage.warning('请选择目标会议')
    return
  }
  await api.post(
    `/meetings/${id}/references`,
    jsonapiResource('meeting-references', {
      targetMeetingId: referenceForm.targetMeetingId,
      note: referenceForm.note || null
    })
  )
  referenceDialogVisible.value = false
  referenceForm.targetMeetingId = undefined
  referenceForm.note = ''
  ElMessage.success('引用已保存')
  await loadReferences()
}

async function submitTrustReview(item: TrustReviewCandidate, verdict: TrustReviewVerdict) {
  if (!item.sentence || trustReviewLoading.value) return
  trustReviewLoading.value = true
  try {
    await api.post(
      `/meetings/${id}/trust-reviews`,
      jsonapiResource('meeting-trust-reviews', {
        sentenceId: item.sentenceId || undefined,
        sentence: item.sentence,
        verdict,
        citationIds: item.citationIds,
        evidenceEventIds: item.evidenceEventIds,
        comment: trustReviewComments[item.key] || ''
      })
    )
    trustReviewComments[item.key] = ''
    ElMessage.success('复核记录已保存')
    await loadMeeting()
  } catch (error) {
    ElMessage.error(apiErrorText(error, '复核记录保存失败'))
  } finally {
    trustReviewLoading.value = false
  }
}

async function submitRecapActionReview(item: MeetingRecapActionSuggestion, decision: RecapActionReviewDecision) {
  const key = recapActionKey(item)
  if (!key || recapActionReviewLoading.value) return
  if (decision === 'approved') {
    try {
      await ElMessageBox.confirm(
        '确认后只记录人工复核结论，不会自动创建自选、唤醒计划或模拟盘订单。',
        '确认复核动作',
        { type: 'warning', confirmButtonText: '确认有效', cancelButtonText: '取消' }
      )
    } catch {
      return
    }
  }
  recapActionReviewLoading.value = true
  try {
    await api.post(
      `/meetings/${id}/recap-action-reviews`,
      jsonapiResource('meeting-recap-action-reviews', {
        suggestionId: item.id,
        sourceEventId: item.eventId,
        actionIndex: item.actionIndex,
        actionType: item.actionType,
        decision,
        citationIds: recapActionCitationIds(item),
        evidenceEventIds: recapActionEvidenceEventIds(item),
        comment: recapActionReviewComments[key] || '',
        confirm: decision === 'approved'
      })
    )
    recapActionReviewComments[key] = ''
    ElMessage.success('动作复核记录已保存')
    await loadMeeting()
  } catch (error) {
    ElMessage.error(apiErrorText(error, '动作复核记录保存失败'))
  } finally {
    recapActionReviewLoading.value = false
  }
}

function payloadStatus(event?: MeetingEvent) {
  return String(event?.payload?.status || '')
}

function dedupeEvents(rows: MeetingEvent[]) {
  const byId = new Map<number, MeetingEvent>()
  for (const row of rows) {
    byId.set(row.id, row)
  }
  return [...byId.values()].sort((left, right) => left.sequence - right.sequence || left.id - right.id)
}

function meetingStatusLabel(value: string | null | undefined) {
  const map: Record<string, string> = {
    queued: '排队中',
    running: '运行中',
    completed: '已完成',
    failed: '失败',
    cancelled: '已取消'
  }
  return map[String(value || '')] || (value ? String(value) : '加载中')
}

function recapStatusLabel(value: string | null | undefined) {
  const map: Record<string, string> = {
    queued: '排队中',
    running: '总结中',
    completed: '已完成',
    failed: '失败',
    cancelled: '已取消'
  }
  return map[String(value || '')] || '-'
}

function triggerSourceLabel(value: string | null | undefined) {
  const map: Record<string, string> = {
    manual: '手动创建',
    telegram: '消息触发',
    wake: '唤醒计划',
    scheduler: '调度器'
  }
  return map[String(value || '')] || value || '-'
}

function wakeStatusLabel(value: string | null | undefined) {
  const map: Record<string, string> = {
    active: '生效中',
    paused: '已暂停',
    fired: '已触发',
    cancelled: '已取消'
  }
  return map[String(value || '')] || value || '-'
}

function wakeTriggerLabel(value: string | null | undefined) {
  const map: Record<string, string> = {
    time: '定时',
    interval: '周期',
    condition: '条件',
    manual: '手动',
    market: '行情',
    news: '消息'
  }
  return map[String(value || '')] || value || '-'
}

function eventStatusLabel(value: string | null | undefined) {
  const map: Record<string, string> = {
    running: '运行中',
    model_call: '调用模型',
    role_started: '角色开始',
    role_completed: '角色完成',
    role_error: '角色失败',
    skipped: '已跳过',
    moderator_kickoff: '主持人开场',
    moderator_review: '主持人复核',
    recap_completed: '总结完成',
    wake_plan_created: '已创建唤醒计划',
    error: '错误',
    conclusion: '结论',
    tool_call: '调用工具',
    tool_result: '工具结果',
    role_message: '角色发言'
  }
  return map[String(value || '')] || value || '-'
}

function payloadText(event: MeetingEvent | undefined, key: string) {
  const value = event?.payload?.[key]
  return typeof value === 'string' ? value : ''
}

function meetingProgress(event?: MeetingEvent) {
  const progress = (event?.payload as { progress?: { current?: unknown; total?: unknown } } | undefined)?.progress
  const current = Number(progress?.current)
  const total = Number(progress?.total)
  if (!Number.isFinite(current) || !Number.isFinite(total) || total <= 0 || current < 0) return null
  return { current, total }
}

function renderMarkdown(content: string) {
  return DOMPurify.sanitize(markdown.render(content || ''))
}

function eventKind(event: MeetingEvent) {
  if (event.type === 'error') return 'error'
  if (event.type === 'conclusion') return 'conclusion'
  if (event.type === 'tool_call' || event.type === 'tool_result') return 'tool'
  if (event.type === 'role_message') return 'role'
  return 'system'
}

function eventTitle(event: MeetingEvent) {
  if (event.type === 'conclusion') return '会议结论'
  if (event.type === 'error') return roleDisplayName(event) || '错误'
  if (event.type === 'tool_call') return roleDisplayName(event) || '工具调用'
  if (event.type === 'tool_result') return roleDisplayName(event) || '工具结果'
  if (event.type === 'role_message') return roleDisplayName(event) || '角色发言'
  return '系统'
}

function roleDisplayName(event: MeetingEvent) {
  const directName = payloadText(event, 'role_name')
  if (directName) return directName
  if (!event.roleKey) return ''
  const started = [...events.value].reverse().find((item) => item.roleKey === event.roleKey && payloadText(item, 'role_name'))
  return payloadText(started, 'role_name') || event.roleKey
}

function initialFor(event: MeetingEvent) {
  const title = eventTitle(event)
  if (event.type === 'conclusion') return '结'
  if (event.type === 'error') return '!'
  if (event.type === 'tool_call' || event.type === 'tool_result') return '工'
  return title.slice(0, 1).toUpperCase()
}

function eventTagType(event: MeetingEvent) {
  if (event.type === 'error' || payloadStatus(event) === 'role_error') return 'danger'
  if (payloadStatus(event) === 'role_completed' || event.type === 'conclusion') return 'success'
  if (event.type === 'tool_call' || event.type === 'tool_result') return 'primary'
  return 'warning'
}

function roleTagType(status: string) {
  if (status === 'role_completed') return 'success'
  if (status === 'role_error') return 'danger'
  if (status === 'model_call') return 'primary'
  return 'warning'
}

function confidenceLabel(value: string | null | undefined) {
  const map: Record<string, string> = {
    unknown: '未知',
    low: '低',
    medium: '中',
    high: '高'
  }
  return map[String(value || 'unknown')] || value || '未知'
}

function confidenceTagType(value: string | null | undefined) {
  const map: Record<string, 'success' | 'warning' | 'danger' | 'info'> = {
    high: 'success',
    medium: 'warning',
    low: 'danger',
    unknown: 'info'
  }
  return map[String(value || 'unknown')] || 'info'
}

function evidenceGateLabel(value: string | null | undefined) {
  return ({
    pass: '已通过',
    warning: '需关注',
    blocked: '已阻断'
  } as Record<string, string>)[String(value || '')] || '需关注'
}

function evidenceGateTagType(value: string | null | undefined) {
  return ({
    pass: 'success',
    warning: 'warning',
    blocked: 'danger'
  } as Record<string, 'success' | 'warning' | 'danger'>)[String(value || '')] || 'warning'
}

function evidenceGateSummary(gate: Record<string, any>) {
  const checked = Number(gate.checkedClaimCount || 0)
  const supported = Number(gate.supportedClaimCount || 0)
  const unsupported = Number(gate.unsupportedClaimCount || 0)
  const status = String(gate.status || 'warning')
  if (status === 'blocked') return `发现 ${unsupported} 条事实/推断缺少证据或引用`
  if (status === 'pass') return `已检查 ${checked} 条事实/推断声明，其中 ${supported} 条已绑定证据`
  if (checked > 0) return `已检查 ${checked} 条事实/推断声明，建议人工复核`
  return '尚无可门禁的事实/推断声明'
}

function currentConclusionSentences() {
  const history = Array.isArray(trustReport.value?.conclusionHistory) ? (trustReport.value?.conclusionHistory as Record<string, any>[]) : []
  const latest = history[history.length - 1] || {}
  const text = String(latest.conclusion || latest.summary || meeting.value?.conclusion || '').trim()
  return splitTrustSentences(text)
}

function splitTrustSentences(text: string) {
  const matches = text.match(/[^.!?;。！？；\n]+[.!?;。！？；]?/g) || []
  return matches.map((item) => item.trim()).filter(Boolean)
}

function matchingTrustBinding(sentence: string) {
  const sentenceKey = normalizeTrustSentenceText(sentence)
  if (!sentenceKey) return null
  const bindings = Array.isArray(trustReport.value?.claimEvidenceBindings) ? (trustReport.value?.claimEvidenceBindings as Record<string, any>[]) : []
  return bindings.find((item) => {
    const claimKey = normalizeTrustSentenceText(String(item.claim || ''))
    return claimKey && (sentenceKey.includes(claimKey) || claimKey.includes(sentenceKey))
  }) || null
}

function normalizeTrustSentenceText(value: string) {
  return String(value || '').replace(/\s+/g, ' ').trim().toLowerCase()
}

function trustStringArray(value: unknown) {
  return Array.isArray(value) ? value.map((item) => String(item).trim()).filter(Boolean) : []
}

function trustNumberArray(value: unknown) {
  if (!Array.isArray(value)) return []
  return value.map((item) => Number(item)).filter((item) => Number.isFinite(item) && item > 0)
}

function recapActionKey(item: MeetingRecapActionSuggestion) {
  return String(item.id || `${item.eventId || 'event'}-${item.actionIndex ?? 0}`)
}

function recapActionLatestReview(item: MeetingRecapActionSuggestion): MeetingRecapActionReviewAttributes | null {
  return item.latestReview || null
}

function recapActionCitationIds(item: MeetingRecapActionSuggestion) {
  const summary = item.evidenceSummary && typeof item.evidenceSummary === 'object'
    ? (item.evidenceSummary as Record<string, unknown>)
    : {}
  return trustStringArray(summary.citations)
}

function recapActionEvidenceEventIds(item: MeetingRecapActionSuggestion) {
  const summary = item.evidenceSummary && typeof item.evidenceSummary === 'object'
    ? (item.evidenceSummary as Record<string, unknown>)
    : {}
  return trustNumberArray(summary.evidence_event_ids ?? summary.evidenceEventIds)
}

function trustEvidenceKey(item: Record<string, any>) {
  return String(item.eventId || item.id || `${item.sequence || 'seq'}-${item.status || 'evidence'}-${item.tool || ''}`)
}

function trustEvidenceStatusLabel(value: unknown) {
  return ({
    tool_result: '工具结果',
    prediction_watchlist_updated: '预测关注已更新',
    watchlist_updated: '自选已更新',
    paper_order_created: '模拟单已创建',
    wake_plan_created: '唤醒已创建'
  } as Record<string, string>)[String(value || '')] || String(value || '证据')
}

function trustEvidenceTagType(value: unknown) {
  return ({
    tool_result: 'primary',
    prediction_watchlist_updated: 'success',
    watchlist_updated: 'success',
    paper_order_created: 'warning',
    wake_plan_created: 'info'
  } as Record<string, 'primary' | 'success' | 'warning' | 'info'>)[String(value || '')] || 'info'
}

function trustEvidenceTitle(item: Record<string, any>) {
  const tool = String(item.tool || '').trim()
  const role = String(item.roleKey || '').trim()
  const eventText = item.eventId ? `事件 #${item.eventId}` : '事件 -'
  const pieces = [tool, role ? `角色 ${role}` : '', eventText].filter(Boolean)
  return pieces.join(' / ')
}

function trustEvidencePreviewText(item: Record<string, any>) {
  const preview = item.preview && typeof item.preview === 'object' ? (item.preview as Record<string, unknown>) : {}
  const rawPreview = typeof item.preview === 'string' ? item.preview : ''
  const parts = [
    compactActionValue('市场ID', preview.market_id ?? preview.marketId),
    compactActionValue('市场', preview.market_question ?? preview.marketQuestion),
    compactActionValue('Market slug', preview.market_slug ?? preview.marketSlug),
    compactActionValue('Event', preview.event_slug ?? preview.eventSlug ?? preview.event_title ?? preview.eventTitle),
    compactActionValue('状态', preview.active === false ? '停用' : preview.active === true ? '关注' : ''),
    compactActionValue('备注', preview.note),
    compactActionValue('预览', rawPreview)
  ].filter(Boolean)
  if (parts.length) return parts.join(' / ')
  try {
    const text = JSON.stringify(item.preview ?? {})
    return text && text !== '{}' ? text.slice(0, 180) : '暂无证据摘要'
  } catch {
    return '暂无证据摘要'
  }
}

function trustEvidenceMarketId(item: Record<string, any>) {
  const preview = item.preview && typeof item.preview === 'object' ? (item.preview as Record<string, unknown>) : {}
  const n = Number(preview.market_id ?? preview.marketId)
  return Number.isFinite(n) && n > 0 ? n : undefined
}

function openPredictionMarketEvidence(item: Record<string, any>) {
  const marketId = trustEvidenceMarketId(item)
  if (!marketId) return
  void router.push({ path: '/prediction-markets', query: { marketId: String(marketId), sourceMeetingId: String(id) } })
}

function trustReviewRefsText(item: TrustReviewCandidate) {
  const evidenceText = item.evidenceEventIds.length ? `证据事件 #${item.evidenceEventIds.join('、')}` : '未绑定证据事件'
  const citationText = item.citationIds.length ? `引用 ${item.citationIds.join('、')}` : '未绑定引用'
  return `${evidenceText}，${citationText}`
}

function recapActionTypeLabel(value: unknown) {
  return ({
    watchlist: '自选',
    prediction_watchlist: '预测关注',
    wake_plan: '唤醒',
    paper_order: '模拟单'
  } as Record<string, string>)[String(value || '')] || String(value || '动作')
}

function recapActionDispositionLabel(value: unknown) {
  return ({
    blocked: '已阻断',
    manual_review_required: '待人工复核'
  } as Record<string, string>)[String(value || '')] || String(value || '待处理')
}

function recapActionTagType(value: unknown) {
  return ({
    blocked: 'danger',
    manual_review_required: 'warning'
  } as Record<string, 'danger' | 'warning' | 'info'>)[String(value || '')] || 'info'
}

function recapActionReviewLabel(value: unknown) {
  return ({
    approved: '已确认',
    needs_evidence: '待补证据',
    rejected: '已驳回',
    superseded: '已替代'
  } as Record<string, string>)[String(value || '')] || '未复核'
}

function recapActionReviewTag(value: unknown) {
  return ({
    approved: 'success',
    needs_evidence: 'warning',
    rejected: 'danger',
    superseded: 'info'
  } as Record<string, 'success' | 'warning' | 'danger' | 'info'>)[String(value || '')] || 'info'
}

function recapActionReviewMeta(review: MeetingRecapActionReviewAttributes | null) {
  if (!review) return '尚未记录人工复核'
  const reviewer = review.reviewer || 'unknown'
  const time = review.reviewedAt ? formatDateTimeUtc8(review.reviewedAt) : '-'
  const note = review.comment ? ` / ${review.comment}` : ''
  return `${reviewer} / ${time}${note}`
}

function recapActionSpecText(value: unknown, actionType?: unknown) {
  const spec = value && typeof value === 'object' ? (value as Record<string, unknown>) : {}
  if (String(actionType || '') === 'prediction_watchlist') {
    const parts = [
      compactActionValue('市场ID', spec.market_id ?? spec.marketId ?? spec.prediction_market_id ?? spec.predictionMarketId),
      compactActionValue('状态', spec.active === false ? '停用' : '关注'),
      compactActionValue('原因', spec.reason ?? spec.note)
    ].filter(Boolean)
    if (parts.length) return parts.join(' / ')
  }
  const parts = [
    compactActionValue('代码', spec.code),
    compactActionValue('名称', spec.name),
    compactActionValue('方向', spec.side),
    compactActionValue('数量', spec.quantity),
    compactActionValue('价格', spec.suggested_price ?? spec.suggestedPrice),
    compactActionValue('触发', spec.trigger_type ?? spec.triggerType),
    compactActionValue('原因', spec.reason ?? spec.note)
  ].filter(Boolean)
  if (parts.length) return parts.join(' / ')
  try {
    const text = JSON.stringify(spec)
    return text && text !== '{}' ? text.slice(0, 180) : '无动作参数'
  } catch {
    return '无动作参数'
  }
}

function compactActionValue(label: string, value: unknown) {
  if (value === null || value === undefined || value === '') return ''
  return `${label}: ${String(value).slice(0, 80)}`
}

function recapActionEvidenceText(value: unknown) {
  const summary = value && typeof value === 'object' ? (value as Record<string, unknown>) : {}
  const facts = trustStringArray(summary.facts).slice(0, 2)
  const inferences = trustStringArray(summary.inferences).slice(0, 2)
  const citations = trustStringArray(summary.citations).slice(0, 3)
  const pieces = [
    facts.length ? `事实 ${facts.join('；')}` : '',
    inferences.length ? `推断 ${inferences.join('；')}` : '',
    citations.length ? `引用 ${citations.join('、')}` : ''
  ].filter(Boolean)
  return pieces.length ? pieces.join(' / ') : '未提供可展示的证据摘要'
}

function reviewVerdictLabel(value: string | null | undefined) {
  return ({
    confirmed: '已确认',
    needs_evidence: '待补证据',
    rejected: '已驳回',
    superseded: '已替代'
  } as Record<string, string>)[String(value || '')] || '未复核'
}

function reviewVerdictTag(value: string | null | undefined) {
  return ({
    confirmed: 'success',
    needs_evidence: 'warning',
    rejected: 'danger',
    superseded: 'info'
  } as Record<string, 'success' | 'warning' | 'danger' | 'info'>)[String(value || '')] || 'info'
}

function trustGapLabel(value: string) {
  const map: Record<string, string> = {
    'no structured tool evidence captured': '缺少结构化工具证据',
    'no structured citations captured': '缺少结构化引用来源',
    'no model snapshot captured': '缺少模型快照'
  }
  return map[value] || value
}

function trustDiffText(item: Record<string, any>) {
  const from = String(item.fromSource || '上版')
  const to = String(item.toSource || '新版')
  const added = Number(item.addedSentenceCount || 0)
  const removed = Number(item.removedSentenceCount || 0)
  const unchanged = Number(item.unchangedSentenceCount || 0)
  const sentenceSummary = `新增 ${added} 句，移除 ${removed} 句，保留 ${unchanged} 句`
  const addedSentences = Array.isArray(item.addedSentences) ? item.addedSentences : []
  const removedSentences = Array.isArray(item.removedSentences) ? item.removedSentences : []
  const firstSentence = String(addedSentences[0] || removedSentences[0] || '').trim()
  if (firstSentence) return `${from} → ${to}：${sentenceSummary}；${firstSentence}`
  const after = String(item.afterPreview || '').trim()
  if (!after) return `${from} → ${to}：${sentenceSummary}`
  return `${from} → ${to}：${sentenceSummary}；${after}`
}

function claimTypeLabel(value: string) {
  return ({
    fact: '事实',
    assumption: '假设',
    inference: '推断',
    evidence_gap: '缺口'
  } as Record<string, string>)[String(value || '')] || '声明'
}

function claimTypeTag(value: string) {
  return ({
    fact: 'success',
    assumption: 'warning',
    inference: 'info',
    evidence_gap: 'danger'
  } as Record<string, string>)[String(value || '')] || 'info'
}

function bindingEvidenceText(item: Record<string, any>) {
  const evidence = Array.isArray(item.evidenceEventIds) ? item.evidenceEventIds : []
  const citations = Array.isArray(item.citationIds) ? item.citationIds : []
  const evidenceText = evidence.length ? `证据事件 #${evidence.join('、#')}` : '无结构化证据事件'
  const citationText = citations.length ? `引用 ${citations.join('、')}` : '无结构化引用'
  return `${evidenceText}；${citationText}`
}

function unsupportedClaimText(item: Record<string, any>) {
  const eventId = item.eventId ? `事件 #${item.eventId}` : '未知事件'
  const reason = String(item.reason || '缺少结构化证据事件和引用')
  return `${eventId}：${reason}`
}

function referenceTypeLabel(type: string) {
  if (type === 'telegram_message') return '采集消息'
  if (type === 'meeting') return '历史会议'
  return type || '引用'
}

function referenceStatusType(item: ChainItem, index: number) {
  if (item.targetDeleted) return 'info'
  if (item.referenceType !== 'meeting') return 'warning'
  if (isLeafReference(index)) return 'success'
  return 'primary'
}

function referenceStatusLabel(item: ChainItem, index: number) {
  if (item.targetDeleted) return '目标已删除'
  if (item.referenceType !== 'meeting') return '外部快照'
  if (isLeafReference(index)) return '链路终点'
  return '继续展开'
}

function canOpenReference(item: ChainItem) {
  return Boolean(!item.targetDeleted && item.targetMeetingId)
}

function isLeafReference(index: number) {
  const current = referenceChain.value[index]
  const next = referenceChain.value[index + 1]
  return !next || next.depth <= current.depth
}

function referencePathText(item: ChainItem, index: number) {
  if (item.targetDeleted) return '引用对象已删除，链路在此保留快照'
  if (item.referenceType === 'telegram_message') return '来自采集消息触发'
  return isLeafReference(index) ? '这是当前可追溯到的链路终点' : '继续引用下游会议'
}

function referenceHint(item: ChainItem, index: number) {
  if (item.targetDeleted) return '目标已删除，不能继续展开'
  if (item.referenceType !== 'meeting') return '保留外部消息快照'
  return isLeafReference(index) ? '没有更深层引用' : '可以继续查看下游会议'
}

function closedReferenceLabel(item: ChainItem, index: number) {
  if (item.targetDeleted) return '已删除节点'
  if (item.referenceType !== 'meeting') return '外部引用'
  if (isLeafReference(index)) return '链路终点'
  return '不可打开'
}

function referenceIndent(depth: number) {
  const unit = isMobile.value ? 8 : 12
  const max = isMobile.value ? 24 : 48
  return `${Math.min(depth * unit, max)}px`
}

onMounted(async () => {
  await loadInitial()
})

useAutoRefresh({
  intervalMs: 2000,
  refresh: async () => {
    await Promise.all([loadMeeting(), loadEvents(true)])
  }
})

useAutoRefresh({
  intervalMs: 10000,
  refresh: async () => {
    await Promise.all([loadWakePlans(), loadReferences()])
  }
})
</script>

<style scoped>
.detail-side,
.detail-side .panel,
.trust-section,
.trust-evidence-item,
.trust-action-item,
.trust-binding-item,
.trust-review-item,
.reference-card {
  min-width: 0;
  max-width: 100%;
}

.detail-side {
  position: sticky;
  top: var(--shell-padding);
  align-self: start;
}

.detail-side-panel {
  display: flex;
  flex-direction: column;
  max-height: calc(100vh - 300px);
  min-height: 420px;
  margin-bottom: 0;
  overflow: hidden;
}

.detail-side-tabs {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  min-height: 0;
}

.detail-side-tabs :deep(.el-tabs__header) {
  flex: 0 0 auto;
  margin-bottom: 14px;
}

.detail-side-tabs :deep(.el-tabs__content) {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  padding-right: 4px;
}

.detail-side-tabs :deep(.el-tab-pane) {
  min-width: 0;
}

.reference-card-deleted {
  border-style: dashed;
  background: var(--surface-muted);
}

.reference-card-leaf {
  box-shadow: inset 0 0 0 1px var(--focus-ring), var(--shadow-card);
}

.reference-card-external {
  border-color: var(--border-strong);
}

.reference-path {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  min-width: 0;
}

.reference-depth {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 36px;
  height: 22px;
  padding: 0 8px;
  border-radius: 999px;
  background: var(--info-soft);
  color: var(--accent-secondary);
  font-size: 12px;
  font-weight: 600;
}

.reference-path-text {
  min-width: 0;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.5;
  overflow-wrap: anywhere;
}

.reference-head strong {
  min-width: 0;
  overflow-wrap: anywhere;
}

.reference-summary {
  margin-top: 6px;
  white-space: pre-wrap;
  word-break: break-word;
}

.reference-foot {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 8px;
  margin-top: 8px;
}

.trust-metrics {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.trust-metrics div {
  min-width: 0;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-panel);
  background: var(--surface-subtle);
  padding: 10px 12px;
}

.trust-metrics span {
  display: block;
  color: var(--text-muted);
  font-size: 12px;
}

.trust-metrics strong {
  color: var(--text-main);
  font-size: 20px;
}

.trust-gate {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  margin-top: 14px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-panel);
  background: var(--surface-subtle);
  padding: 10px 12px;
}

.trust-gate > div {
  min-width: 0;
}

.trust-gate .el-tag {
  flex: 0 0 auto;
}

.trust-gate strong {
  display: block;
  color: var(--text-main);
  font-size: 13px;
}

.trust-gate span {
  display: block;
  margin-top: 3px;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.45;
  overflow-wrap: anywhere;
}

.trust-gate-pass {
  border-color: color-mix(in srgb, var(--success) 38%, var(--border-soft));
  background: var(--success-soft);
}

.trust-gate-warning {
  border-color: var(--attention-border);
  background: var(--attention-soft);
}

.trust-gate-blocked {
  border-color: color-mix(in srgb, var(--danger) 38%, var(--border-soft));
  background: var(--danger-soft);
}

.trust-section {
  display: grid;
  gap: 8px;
  margin-top: 14px;
}

.trust-claims {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
}

.trust-claims .el-tag {
  max-width: 100%;
  height: auto;
  min-height: 24px;
  align-items: flex-start;
  padding: 4px 8px;
  white-space: normal;
}

.trust-claims :deep(.el-tag__content) {
  min-width: 0;
  white-space: normal;
  overflow-wrap: anywhere;
  word-break: break-word;
}

.trust-gap-list {
  margin: 0;
  padding-left: 18px;
  color: var(--text-muted);
  overflow-wrap: anywhere;
}

.trust-diff-list {
  display: grid;
  gap: 8px;
}

.trust-binding-list {
  display: grid;
  gap: 8px;
}

.trust-binding-item {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 6px 8px;
  align-items: start;
  color: var(--text-muted);
  font-size: 13px;
  line-height: 1.45;
}

.trust-binding-item .muted {
  grid-column: 2 / -1;
  min-width: 0;
  overflow-wrap: anywhere;
}

.trust-binding-claim {
  min-width: 0;
  overflow-wrap: anywhere;
}

.trust-unsupported-item {
  color: var(--danger);
}

.trust-diff-item {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  color: var(--text-muted);
  font-size: 13px;
  line-height: 1.45;
  min-width: 0;
}

.trust-diff-item span {
  min-width: 0;
  overflow-wrap: anywhere;
}

.trust-action-list {
  display: grid;
  gap: 10px;
}

.trust-evidence-list {
  display: grid;
  gap: 10px;
}

.trust-evidence-item {
  display: grid;
  gap: 6px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-panel);
  background: var(--surface-subtle);
  padding: 10px;
}

.trust-evidence-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  line-height: 1.4;
  min-width: 0;
}

.trust-evidence-head .muted {
  min-width: 0;
  overflow-wrap: anywhere;
}

.trust-evidence-preview {
  min-width: 0;
  color: var(--text-main);
  font-size: 13px;
  line-height: 1.45;
  overflow-wrap: anywhere;
}

.trust-action-item {
  display: grid;
  gap: 6px;
  border: 1px solid var(--attention-border);
  border-radius: var(--radius-panel);
  background: var(--attention-soft);
  padding: 10px;
}

.trust-action-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  line-height: 1.4;
  min-width: 0;
}

.trust-action-head .muted {
  min-width: 0;
  overflow-wrap: anywhere;
}

.trust-action-spec {
  min-width: 0;
  color: var(--text-main);
  font-size: 13px;
  line-height: 1.45;
  overflow-wrap: anywhere;
}

.trust-action-reason {
  min-width: 0;
  overflow-wrap: anywhere;
}

.trust-action-evidence {
  min-width: 0;
  overflow-wrap: anywhere;
}

.trust-action-review {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  line-height: 1.4;
  min-width: 0;
}

.trust-action-review .muted {
  min-width: 0;
  overflow-wrap: anywhere;
}

.trust-review-list {
  display: grid;
  gap: 10px;
}

.trust-review-item {
  display: grid;
  gap: 8px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-panel);
  background: var(--surface-subtle);
  padding: 10px;
}

.trust-review-sentence {
  min-width: 0;
  color: var(--text-main);
  font-size: 13px;
  line-height: 1.5;
  overflow-wrap: anywhere;
}

.trust-review-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  line-height: 1.4;
  min-width: 0;
}

.trust-review-meta .muted {
  min-width: 0;
  overflow-wrap: anywhere;
}

@media (max-width: 767px) {
  .trust-binding-item {
    grid-template-columns: 1fr;
  }

  .trust-binding-item .muted {
    grid-column: 1;
  }
}

@media (max-width: 1200px) {
  .detail-side {
    position: static;
  }

  .detail-side-panel {
    max-height: none;
    min-height: 0;
    overflow: visible;
  }

  .detail-side-tabs :deep(.el-tabs__content) {
    overflow: visible;
    padding-right: 0;
  }
}
</style>
