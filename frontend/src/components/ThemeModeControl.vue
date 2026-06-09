<template>
  <div class="theme-control">
    <div class="theme-control-row">
      <el-popover
        v-model:visible="paletteOpen"
        trigger="click"
        placement="bottom-start"
        popper-class="theme-palette-popover"
        :width="220"
      >
        <template #reference>
          <el-button
            class="theme-palette-trigger"
            plain
            :aria-label="`系统配色：${currentPalette.label}`"
            :title="`系统配色：${currentPalette.label}`"
          >
            <el-icon><Brush /></el-icon>
            <span class="theme-palette-swatch" :style="{ background: currentPalette.swatch }"></span>
          </el-button>
        </template>

        <div class="theme-palette-panel">
          <div class="theme-palette-panel__head">
            <el-icon><Brush /></el-icon>
            <span>系统配色</span>
          </div>
          <el-radio-group
            v-model="themePalette"
            class="theme-palette-control"
            size="small"
            aria-label="系统配色"
            @change="closePalette"
          >
            <el-radio-button
              v-for="item in paletteOptions"
              :key="item.value"
              :label="item.value"
              @click="closePalette"
            >
              <span class="theme-palette-swatch" :style="{ background: item.swatch }"></span>
              <span>{{ item.label }}</span>
            </el-radio-button>
          </el-radio-group>
        </div>
      </el-popover>

      <el-radio-group v-model="themeMode" class="theme-mode-control" size="small" aria-label="主题模式">
        <el-radio-button label="system">
          <el-icon><Monitor /></el-icon>
          <span>系统</span>
        </el-radio-button>
        <el-radio-button label="light">
          <el-icon><Sunny /></el-icon>
          <span>浅色</span>
        </el-radio-button>
        <el-radio-button label="dark">
          <el-icon><Moon /></el-icon>
          <span>深色</span>
        </el-radio-button>
      </el-radio-group>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Brush, Monitor, Moon, Sunny } from '@element-plus/icons-vue'
import { useTheme, type ThemePalette } from '../composables/useTheme'

const { themeMode, themePalette } = useTheme()
const paletteOpen = ref(false)

const paletteOptions = [
  { value: 'emerald', label: '翠绿', swatch: 'linear-gradient(135deg, #14b8a6, #3b82f6)' },
  { value: 'blue', label: '海蓝', swatch: 'linear-gradient(135deg, #2563eb, #06b6d4)' },
  { value: 'violet', label: '靛紫', swatch: 'linear-gradient(135deg, #7c3aed, #ec4899)' },
  { value: 'coral', label: '珊瑚', swatch: 'linear-gradient(135deg, #f97316, #e11d48)' },
  { value: 'graphite', label: '石墨', swatch: 'linear-gradient(135deg, #475569, #0ea5e9)' }
] satisfies Array<{ value: ThemePalette; label: string; swatch: string }>

const currentPalette = computed(() => {
  return paletteOptions.find((item) => item.value === themePalette.value) ?? paletteOptions[0]
})

watch(themePalette, closePalette)

function closePalette() {
  paletteOpen.value = false
}
</script>
