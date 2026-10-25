<script setup lang="ts">
// 应用外壳：标题栏（可拖动）+ 导航 + 语言切换 + 内容区。
//
// 因为窗口用了 MacTitleBarHiddenInset，红绿灯浮在内容上，
// 所以顶部留 40px 的拖动条，并给它一个半透明背景挡住滚动内容。
import { onMounted, onUnmounted, watch } from "vue"
import { useI18n } from "vue-i18n"
import { useRoute, useRouter } from "vue-router"
import { Languages } from "lucide-vue-next"
import Button from "@/components/ui/Button.vue"
import { detectLocale, setLocale, type Locale } from "@/locales"
import { onEvent, EventName } from "@/api"
import { useAppStore } from "@/stores/app"

const { t, locale } = useI18n()
const store = useAppStore()
const route = useRoute()
const router = useRouter()

/** 三个页面的导航项：path 同时是路由和 i18n key 的一部分。 */
const navItems = [
  { path: "/", key: "nav.switcher" },
  { path: "/sources", key: "nav.sources" },
  { path: "/settings", key: "nav.settings" },
] as const

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
  <div class="flex h-full flex-col">
    <!-- 拖动区：高度 40px 对应 InvisibleTitleBarHeight；左侧让开红绿灯 -->
    <header class="drag-region flex h-10 shrink-0 items-center justify-between pl-20 pr-3">
      <nav class="no-drag flex items-center gap-1">
        <RouterLink
          v-for="item in navItems"
          :key="item.path"
          :to="item.path"
          class="rounded-md px-2.5 py-1 text-xs transition-colors"
          :class="
            route.path === item.path
              ? 'bg-secondary font-medium text-foreground'
              : 'text-muted-foreground hover:text-foreground'
          "
        >
          {{ t(item.key) }}
        </RouterLink>
      </nav>
      <Button variant="ghost" size="icon" class="no-drag" :title="locale" @click="toggleLocale">
        <Languages class="size-4" />
      </Button>
    </header>

    <main class="min-h-0 flex-1 overflow-y-auto">
      <RouterView />
    </main>
  </div>
</template>
