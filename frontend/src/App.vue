<template>
  <router-view v-if="route.path === '/login'" />
  <template v-else>
    <header v-if="isMobileNav" class="mobile-header">
      <div class="mobile-header__brand">
        <span class="brand-mark">投</span>
        <div>
          <strong>TradingCopilot</strong>
          <small>{{ currentMenuLabel }}</small>
        </div>
      </div>
      <el-button circle class="mobile-menu-button" @click="mobileMenuOpen = true">
        <el-icon><Menu /></el-icon>
      </el-button>
    </header>

    <el-container class="shell" :class="{ 'shell-mobile': isMobileNav }">
      <el-aside v-if="!isMobileNav" width="248px" class="sidebar">
        <div class="brand">
          <span class="brand-mark">投</span>
          <div>
            <strong>TradingCopilot</strong>
            <small>智能决策工作台</small>
          </div>
        </div>
        <el-menu router :default-active="menuActivePath" class="menu">
          <template v-for="section in menuSections" :key="section.label">
            <div class="menu-section-label">{{ section.label }}</div>
            <el-menu-item v-for="item in section.items" :key="item.path" :index="item.path">
              <el-icon><component :is="item.icon" /></el-icon>
              <span>{{ item.label }}</span>
            </el-menu-item>
          </template>
        </el-menu>
        <div class="sidebar-actions">
          <ThemeModeControl />
          <el-button :icon="SwitchButton" plain @click="logout">退出</el-button>
        </div>
      </el-aside>
      <el-main class="content">
        <router-view />
      </el-main>
    </el-container>

    <el-drawer v-model="mobileMenuOpen" direction="ltr" size="280px" :with-header="false" class="mobile-nav-drawer">
      <div class="brand">
        <span class="brand-mark">投</span>
        <div>
          <strong>TradingCopilot</strong>
          <small>智能决策工作台</small>
        </div>
      </div>
      <el-menu :default-active="menuActivePath" class="menu" @select="handleMenuSelect">
        <template v-for="section in menuSections" :key="section.label">
          <div class="menu-section-label">{{ section.label }}</div>
          <el-menu-item v-for="item in section.items" :key="item.path" :index="item.path">
            <el-icon><component :is="item.icon" /></el-icon>
            <span>{{ item.label }}</span>
          </el-menu-item>
        </template>
      </el-menu>
      <div class="sidebar-actions">
        <ThemeModeControl />
        <el-button :icon="SwitchButton" plain @click="logout">退出</el-button>
      </div>
    </el-drawer>
  </template>
</template>

<script setup lang="ts">
import {
  Bell,
  ChatLineRound,
  Connection,
  Cpu,
  DataBoard,
  Menu,
  Message,
  Monitor,
  OfficeBuilding,
  Tickets,
  Setting,
  SwitchButton,
  Timer,
  TrendCharts,
  UserFilled,
  Wallet
} from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, apiErrorText } from './api'
import { clearAuthToken } from './auth'
import ThemeModeControl from './components/ThemeModeControl.vue'
import { useResponsive } from './composables/useResponsive'

const router = useRouter()
const route = useRoute()
const { isMobileNav } = useResponsive()
const mobileMenuOpen = ref(false)

const menuSections = [
  {
    label: '使用',
    items: [
      { path: '/', label: '总览', icon: DataBoard },
      { path: '/meetings', label: '会议', icon: ChatLineRound },
      { path: '/ingested-messages', label: '消息库', icon: Message },
      { path: '/message-subscriptions', label: '消息订阅器', icon: Bell },
      { path: '/paper', label: '模拟盘', icon: Wallet },
      { path: '/wake', label: '唤醒计划', icon: Timer },
      { path: '/research-team', label: '投研团队', icon: OfficeBuilding },
      { path: '/market', label: '行情与自选', icon: TrendCharts },
      { path: '/prediction-markets', label: '预测市场', icon: TrendCharts }
    ]
  },
  {
    label: '管理',
    items: [
      { path: '/setup', label: '设置向导', icon: Setting },
      { path: '/model-providers', label: '模型提供商', icon: Cpu },
      { path: '/platform-adapters', label: '平台适配器', icon: Connection },
      { path: '/settings', label: '系统设置', icon: Setting },
      { path: '/admin', label: '团队安全', icon: UserFilled },
      { path: '/ops', label: '运维状态', icon: Monitor }
    ]
  },
  {
    label: '日志',
    items: [
      { path: '/logs', label: '系统日志', icon: Tickets }
    ]
  }
]
const menuItems = menuSections.flatMap((section) => section.items)

const menuActivePath = computed(() => {
  if (route.path.startsWith('/meetings')) return '/meetings'
  return route.path
})

const currentMenuLabel = computed(() => {
  return menuItems.find((item) => item.path === menuActivePath.value)?.label || 'TradingCopilot'
})

watch(
  () => route.fullPath,
  () => {
    mobileMenuOpen.value = false
  }
)

function handleMenuSelect(path: string) {
  router.push(path)
}

async function logout() {
  try {
    await api.post('/auth/logout')
  } catch (error) {
    ElMessage.warning(apiErrorText(error, '退出时未能撤销服务端会话'))
  } finally {
    clearAuthToken()
    router.replace('/login')
  }
}
</script>
