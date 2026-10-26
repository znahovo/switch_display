<script setup lang="ts">
// 应用外壳：左侧可展开导航栏 + 内容区。
//
// 为什么是左侧栏而不是顶部横排：这个应用只有三个页面，但「切换台」是常驻
// 主操作，竖向排列能给每个入口配图标 + 文字，标题栏也不必再挤一层。
// 窗口在 macOS 上用了 MacTitleBarHiddenInset，红绿灯浮在左上角，所以侧栏
// 顶部留 40px 拖动区，红绿灯正好落在侧栏里；Windows 保留原生标题栏（关窗
// 行为按系统习惯走），那 40px 就只剩一块空白，所以按平台隐藏。
import { onMounted, onUnmounted, ref, watch } from "vue"
import { useI18n } from "vue-i18n"
import { useRoute, useRouter } from "vue-router"
import {
  ChevronsLeft,
  ChevronsRight,
  Languages,
  LayoutGrid,
  ListVideo,
  Settings as SettingsIcon,
} from "lucide-vue-next"
import { detectLocale, setLocale, type Locale } from "@/locales"
import { isMac } from "@/lib/platform"
import { onEvent, EventName } from "@/api"
import { useAppStore } from "@/stores/app"

const { t, locale } = useI18n()
const store = useAppStore()
const route = useRoute()
const router = useRouter()

/** 三个页面的导航项：path 同时是路由和 i18n key 的一部分。 */
const navItems = [
  { path: "/", key: "nav.switcher", icon: LayoutGrid },
  { path: "/sources", key: "nav.sources", icon: ListVideo },
  { path: "/settings", key: "nav.settings", icon: SettingsIcon },
] as const

/**
 * 折叠状态存 localStorage：窗口小、用户可能偏爱图标栏，
 * 但每次启动都回到展开态会很烦。
 */
const COLLAPSE_KEY = "switch-display:nav-collapsed"
const collapsed = ref(localStorage.getItem(COLLAPSE_KEY) === "1")
watch(collapsed, (v) => localStorage.setItem(COLLAPSE_KEY, v ? "1" : "0"))

const offs: Array<() => void> = []

onMounted(() => {
  // 后端状态变化（托盘、快捷键、导入、热插拔）时整页重拉，
  // 保证 UI 与磁盘/硬件一致，而不是各页各猜。
  offs.push(onEvent(EventName.stateChanged, () => store.refresh()))
  // 切换结果由后端推过来：这里只记 outcome，细节文案已在 DTO 里。
  offs.push(onEvent(EventName.switchFinished, () => store.refresh()))
  // 托盘点「设置…」时把用户带到设置页（窗口由后端显示出来）。
  offs.push(onEvent(EventName.openSettings, () => router.push("/settings")))
  // 通用跳转：payload 是目标路径。
  offs.push(
    onEvent<string>(EventName.showView, (path) => {
      if (path) router.push(path)
    }),
  )
})

onUnmounted(() => offs.forEach((off) => off()))

// 配置里的 language 是「界面语言」的真相：启动读回配置后要把 i18n 切过去，
// 否则用户在上次会话里选的语言重启后又变回系统语言。
watch(
  () => store.settings.language,
  (lang) => setLocale(detectLocale(lang)),
)

function toggleLocale() {
  const next: Locale = locale.value === "zh-CN" ? "en" : "zh-CN"
  setLocale(next)
}
</script>

<template>
  <div class="flex h-full">
    <!-- 侧栏：顶部 40px 与 InvisibleTitleBarHeight 对齐，红绿灯落在这里 -->
    <aside
      class="flex shrink-0 flex-col border-r border-border/70 bg-muted/35 transition-[width] duration-150"
      :class="collapsed ? 'w-[60px]' : 'w-[204px]'"
    >
      <!-- 红绿灯区域：只负责拖动，不放内容（仅 macOS 有红绿灯） -->
      <div v-if="isMac" class="drag-region h-10 shrink-0" />
      <!-- 其它平台没有红绿灯，用一小段内边距代替那 40px -->
      <div v-else class="h-2 shrink-0" />

      <nav class="no-drag flex flex-col gap-0.5 px-2">
        <RouterLink
          v-for="item in navItems"
          :key="item.path"
          :to="item.path"
          class="group flex h-9 items-center gap-2.5 rounded-lg px-2.5 text-[13px] transition-colors"
          :class="
            route.path === item.path
              ? 'bg-background font-medium text-foreground shadow-xs ring-1 ring-border/70'
              : 'text-muted-foreground hover:bg-background/60 hover:text-foreground'
          "
          :title="collapsed ? t(item.key) : undefined"
        >
          <component
            :is="item.icon"
            class="size-4 shrink-0"
            :class="route.path === item.path ? 'text-foreground' : 'text-muted-foreground'"
          />
          <span v-if="!collapsed" class="truncate">{{ t(item.key) }}</span>
        </RouterLink>
      </nav>

      <div class="flex-1" />

      <!-- 底部：折叠开关 + 语言。两者都是「应用级」操作，放最低处不抢注意力 -->
      <div class="no-drag flex flex-col gap-0.5 border-t border-border/70 px-2 py-2">
        <button
          class="flex h-8 items-center gap-2.5 rounded-lg px-2.5 text-[13px] text-muted-foreground transition-colors hover:bg-background/60 hover:text-foreground"
          :title="collapsed ? t('nav.expand') : t('nav.collapse')"
          @click="collapsed = !collapsed"
        >
          <component :is="collapsed ? ChevronsRight : ChevronsLeft" class="size-4 shrink-0" />
          <span v-if="!collapsed" class="truncate">{{ t("nav.collapse") }}</span>
        </button>
        <button
          class="flex h-8 items-center gap-2.5 rounded-lg px-2.5 text-[13px] text-muted-foreground transition-colors hover:bg-background/60 hover:text-foreground"
          :title="locale"
          @click="toggleLocale"
        >
          <Languages class="size-4 shrink-0" />
          <span v-if="!collapsed" class="truncate">
            {{ locale === "zh-CN" ? "简体中文" : "English" }}
          </span>
        </button>
      </div>
    </aside>

    <main class="min-h-0 min-w-0 flex-1 overflow-y-auto">
      <RouterView />
    </main>
  </div>
</template>
