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
          <el-menu-item v-for="item in menuItems" :key="item.path" :index="item.path">
            <el-icon><component :is="item.icon" /></el-icon>
            <span>{{ item.label }}</span>
          </el-menu-item>
        </el-menu>
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
        <el-menu-item v-for="item in menuItems" :key="item.path" :index="item.path">
          <el-icon><component :is="item.icon" /></el-icon>
          <span>{{ item.label }}</span>
        </el-menu-item>
      </el-menu>
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
  OfficeBuilding,
  Tickets,
  Setting,
  Timer,
  TrendCharts,
  Wallet
} from '@element-plus/icons-vue'
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useResponsive } from './composables/useResponsive'

const router = useRouter()
const route = useRoute()
const { isMobileNav } = useResponsive()
const mobileMenuOpen = ref(false)

const menuItems = [
  { path: '/', label: '总览', icon: DataBoard },
  { path: '/meetings', label: '会议', icon: ChatLineRound },
  { path: '/ingested-messages', label: '消息库', icon: Message },
  { path: '/paper', label: '模拟盘', icon: Wallet },
  { path: '/wake', label: '唤醒计划', icon: Timer },
  { path: '/research-team', label: '投研团队', icon: OfficeBuilding },
  { path: '/market', label: '行情与自选', icon: TrendCharts },
  { path: '/message-subscriptions', label: '消息订阅器', icon: Bell },
  { path: '/model-providers', label: '模型提供商', icon: Cpu },
  { path: '/platform-adapters', label: '平台适配器', icon: Connection },
  { path: '/settings', label: '系统设置', icon: Setting },
  { path: '/logs', label: '系统日志', icon: Tickets }
]

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
</script>
