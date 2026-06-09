import { createRouter, createWebHistory } from 'vue-router'
import { TOKEN_STORAGE_KEY, clearAuthToken, getAuthToken, getAuthTokenExpiresAt, isAuthTokenUsable } from './auth'

const DashboardView = () => import('./views/DashboardView.vue')
const LoginView = () => import('./views/LoginView.vue')
const SetupView = () => import('./views/SetupView.vue')
const AdminView = () => import('./views/AdminView.vue')
const OpsView = () => import('./views/OpsView.vue')
const SettingsView = () => import('./views/SettingsView.vue')
const ModelProvidersView = () => import('./views/ModelProvidersView.vue')
const ResearchTeamView = () => import('./views/ResearchTeamView.vue')
const MarketView = () => import('./views/MarketView.vue')
const MessageSubscriptionsView = () => import('./views/MessageSubscriptionsView.vue')
const PlatformAdaptersView = () => import('./views/PlatformAdaptersView.vue')
const IngestedMessagesView = () => import('./views/IngestedMessagesView.vue')
const MeetingsView = () => import('./views/MeetingsView.vue')
const MeetingDetailView = () => import('./views/MeetingDetailView.vue')
const PaperView = () => import('./views/PaperView.vue')
const WakeView = () => import('./views/WakeView.vue')
const LogsView = () => import('./views/LogsView.vue')

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: LoginView },
    { path: '/', component: DashboardView },
    { path: '/setup', component: SetupView },
    { path: '/admin', component: AdminView },
    { path: '/ops', component: OpsView },
    { path: '/settings', component: SettingsView },
    { path: '/model-providers', component: ModelProvidersView },
    { path: '/research-team', component: ResearchTeamView },
    { path: '/market', component: MarketView },
    { path: '/telegram', redirect: '/message-subscriptions' },
    { path: '/message-subscriptions', component: MessageSubscriptionsView },
    { path: '/platform-adapters', component: PlatformAdaptersView },
    { path: '/ingested-messages', component: IngestedMessagesView },
    { path: '/meetings', component: MeetingsView },
    { path: '/meetings/:id', component: MeetingDetailView },
    { path: '/paper', component: PaperView },
    { path: '/wake', component: WakeView },
    { path: '/logs', component: LogsView }
  ]
})

router.beforeEach((to) => {
  if (to.path !== '/login' && !isAuthTokenUsable(getAuthToken())) {
    clearAuthToken()
    return { path: '/login', query: { redirect: to.fullPath } }
  }
})

let authExpiryTimer: ReturnType<typeof setTimeout> | undefined

router.afterEach(() => {
  scheduleAuthExpiryRedirect()
})

window.addEventListener('storage', (event) => {
  if (event.key === TOKEN_STORAGE_KEY || event.key === 'tradingcopilot_token') {
    scheduleAuthExpiryRedirect()
  }
})

function scheduleAuthExpiryRedirect() {
  if (authExpiryTimer) {
    clearTimeout(authExpiryTimer)
    authExpiryTimer = undefined
  }
  const expiresAt = getAuthTokenExpiresAt(getAuthToken())
  if (!expiresAt) return
  const delay = expiresAt - Date.now()
  if (delay <= 0) {
    redirectToLogin()
    return
  }
  authExpiryTimer = setTimeout(redirectToLogin, delay)
}

function redirectToLogin() {
  clearAuthToken()
  const current = router.currentRoute.value
  if (current.path !== '/login') {
    router.replace({ path: '/login', query: { redirect: current.fullPath } })
  }
}
