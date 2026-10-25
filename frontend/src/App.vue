<script setup lang="ts">
// 应用外壳：标题栏（可拖动）+ 语言切换 + 内容区。
//
// 因为窗口用了 MacTitleBarHiddenInset，红绿灯浮在内容上，
// 所以顶部留 40px 的拖动条，并给它一个半透明背景挡住滚动内容。
import { onMounted, onUnmounted, ref } from "vue"
import { useI18n } from "vue-i18n"
import { Languages } from "lucide-vue-next"
import SwitcherView from "@/views/SwitcherView.vue"
import Button from "@/components/ui/Button.vue"
import { setLocale, type Locale } from "@/locales"
import { onEvent, EventName } from "@/api"
import { useAppStore } from "@/stores/app"

const { t, locale } = useI18n()
const store = useAppStore()

const offs: Array<() => void> = []

onMounted(() => {
  // 后端状态变化（托盘、快捷键、导入、热插拔）时整页重拉，
  // 保证 UI 与磁盘/硬件一致，而不是各页各猜。
  offs.push(onEvent(EventName.stateChanged, () => store.refresh()))
  // 切换结果由后端推过来：这里只记 outcome，细节文案已在 DTO 里。
  offs.push(onEvent(EventName.switchFinished, () => store.refresh()))
})

onUnmounted(() => offs.forEach((off) => off()))

function toggleLocale() {
  const next: Locale = locale.value === "zh-CN" ? "en" : "zh-CN"
  setLocale(next)
}
</script>

<template>
  <div class="flex h-full flex-col">
    <!-- 拖动区：高度 40px 对应 InvisibleTitleBarHeight -->
    <header class="drag-region flex h-10 shrink-0 items-center justify-end gap-1 px-3">
      <Button variant="ghost" size="icon" class="no-drag" :title="locale" @click="toggleLocale">
        <Languages class="size-4" />
      </Button>
    </header>

    <main class="min-h-0 flex-1 overflow-y-auto">
      <SwitcherView />
    </main>
  </div>
</template>
