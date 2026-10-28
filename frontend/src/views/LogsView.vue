<script setup lang="ts">
// 日志页：把应用自身日志与 deskflow-core 子进程的原始输出摆在一起。
//
// 这个页面存在的理由很具体：打包后的 .app/.exe 是双击打开的，没有终端，
// 而共享键鼠依赖一个外部进程（deskflow-core）。用户在 Windows 上看到
// 「启动中」一直不动时，唯一能定位原因的线索就是子进程自己说的话。
//
// 三个刻意的取舍：
// 1) 自动跟随底部，但只要用户手动往上滚就停——否则正在看报错时
//    新日志会把视线拽走。
// 2) 级别/来源是「标签式」筛选而不是下拉：排障时反复切换，
//    下拉要点两次，标签只要一次。
// 3) 复制按钮直接给整段纯文本，用户粘到 issue 里就能用。
import { computed, onMounted, onUnmounted, ref, watch } from "vue"
import { useI18n } from "vue-i18n"
import { ArrowDownToLine, Check, Copy, Eraser, Pause, Play, RefreshCw, ScrollText } from "lucide-vue-next"
import PageShell from "@/components/PageShell.vue"
import VirtualList from "@/components/VirtualList.vue"
import Badge from "@/components/ui/Badge.vue"
import Button from "@/components/ui/Button.vue"
import Card from "@/components/ui/Card.vue"
import Checkbox from "@/components/ui/Checkbox.vue"
import { EventName, onEvent } from "@/api"
import { useLogStore } from "@/stores/log"

const { t } = useI18n()
const store = useLogStore()

/** 虚拟列表对外只暴露两个动作；泛型组件没有可用的 InstanceType，手写更快也更清楚。 */
type ListHandle = { scrollToBottom: () => void; isAtBottom: () => boolean }
const list = ref<ListHandle | null>(null)
const copied = ref(false)
/** 暂停后不再自动拉取：用户想静止看某一段时用。 */
const paused = ref(false)
let off: (() => void) | null = null

const levels = computed(() => [
  { key: "debug", label: t("logs.levelDebug"), tone: "text-muted-foreground" },
  { key: "info", label: t("logs.levelInfo"), tone: "text-foreground/80" },
  { key: "warn", label: t("logs.levelWarn"), tone: "text-amber-600 dark:text-amber-400" },
  { key: "error", label: t("logs.levelError"), tone: "text-destructive" },
  { key: "raw", label: t("logs.levelRaw"), tone: "text-muted-foreground" },
])

onMounted(async () => {
  await store.refresh(true)
  // 后端把 200ms 内的多条日志合成一次事件；收到就补拉增量。
  off = onEvent(EventName.logAppended, () => {
    if (!paused.value) void store.refresh()
  })
})

onUnmounted(() => off?.())

/**
 * 跟随：有新内容就滚到底。
 *
 * 只订阅「可见条数」而不是整条数组：内容没变时后端仍会每 200ms 发一次事件，
 * 每次都滚一下会让用户手动翻看历史时被反复拽回去。
 */
watch(
  () => store.visible.length,
  () => {
    if (store.follow && !paused.value) list.value?.scrollToBottom()
  },
  // post：等新行进了 DOM 再滚，否则拿到的是旧的 scrollHeight，
  // 只会停在上一批的最后一行的位置。
  { flush: "post" },
)

/** 列表回报是否贴底：贴着就继续跟随，手动往上滚就停。 */
function onAtBottom(atBottom: boolean) {
  store.follow = atBottom
}

function scrollToBottom() {
  store.follow = true
  list.value?.scrollToBottom()
}

async function copyAll() {
  if (await store.copyAll()) {
    copied.value = true
    setTimeout(() => (copied.value = false), 1600)
  }
}

/** 每行的时间只留 HH:MM:SS.mmm：日期在同一份日志里几乎总是一样的。 */
function shortTime(t0: string): string {
  const i = t0.indexOf("T")
  return i >= 0 ? t0.slice(i + 1) : t0
}

/** 级别文案，未匹配时原样显示。 */
function levelLabel(level: string): string {
  const hit = levels.value.find((l) => l.key === level)
  return hit ? hit.label : level.toUpperCase()
}

function levelTone(level: string): string {
  const hit = levels.value.find((l) => l.key === level)
  return hit ? hit.tone : "text-muted-foreground"
}
</script>

<template>
  <PageShell :title="t('logs.title')" :hint="t('logs.hint')">
    <template #actions>
      <Button variant="outline" size="sm" :disabled="store.loading" @click="store.refresh(true)">
        <RefreshCw class="size-3.5" />
        {{ t("common.refresh") }}
      </Button>
      <Button variant="outline" size="sm" @click="copyAll">
        <Check v-if="copied" class="size-3.5 text-emerald-600" />
        <Copy v-else class="size-3.5" />
        {{ t("logs.copy") }}
      </Button>
      <Button variant="outline" size="sm" @click="store.clear()">
        <Eraser class="size-3.5" />
        {{ t("logs.clear") }}
      </Button>
    </template>

    <!-- Deskflow 没起来时，这段提示比任何日志都先被看懂 -->
    <Card v-if="store.hasError" class="border-destructive/30 bg-destructive/[0.05] px-4 py-3">
      <p class="text-[13px] font-medium text-destructive">{{ t("logs.hasError") }}</p>
      <p class="mt-1 text-[12px] leading-relaxed text-muted-foreground">
        {{ t("logs.hasErrorHint") }}
      </p>
    </Card>

    <!-- 筛选条 -->
    <Card class="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-2.5">
      <div class="flex flex-wrap items-center gap-1">
        <span class="mr-1 text-[11px] text-muted-foreground">{{ t("logs.filterLevel") }}</span>
        <button
          v-for="lv in levels"
          :key="lv.key"
          class="rounded-md px-1.5 py-0.5 text-[11px] transition-colors"
          :class="
            store.level === lv.key
              ? 'bg-primary text-primary-foreground'
              : 'text-muted-foreground hover:bg-accent'
          "
          @click="store.setLevel(lv.key)"
        >
          {{ lv.label }}
        </button>
      </div>

      <div v-if="store.sources.length" class="flex flex-wrap items-center gap-1">
        <span class="mr-1 text-[11px] text-muted-foreground">{{ t("logs.filterSource") }}</span>
        <button
          v-for="src in store.sources"
          :key="src"
          class="rounded-md px-1.5 py-0.5 font-mono text-[11px] transition-colors"
          :class="
            store.source === src
              ? 'bg-primary text-primary-foreground'
              : 'text-muted-foreground hover:bg-accent'
          "
          @click="store.setSource(src)"
        >
          {{ src }}
        </button>
      </div>

      <div class="ml-auto flex items-center gap-2.5">
        <!-- 默认勾上：Wails 每个资源请求/绑定调用都打一条，混进来就找不到自己的日志了 -->
        <label class="flex cursor-pointer items-center gap-1.5 text-[11px] text-muted-foreground">
          <Checkbox v-model="store.hideFramework" class="size-3.5" />
          {{ t("logs.hideFramework") }}
        </label>
        <label class="flex cursor-pointer items-center gap-1.5 text-[11px] text-muted-foreground">
          <Checkbox v-model="paused" class="size-3.5" />
          <Pause v-if="paused" class="size-3" />
          <Play v-else class="size-3" />
          {{ t("logs.pause") }}
        </label>
        <Button variant="ghost" size="sm" @click="scrollToBottom">
          <ArrowDownToLine class="size-3.5" />
          {{ t("logs.bottom") }}
        </Button>
      </div>
    </Card>

    <!-- 日志正文：虚拟滚动，DOM 里只有视口那几十行 -->
    <Card class="overflow-hidden p-0">
      <div v-if="!store.visible.length" class="flex flex-col items-center gap-2 px-6 py-16 text-center">
        <ScrollText class="size-5 text-muted-foreground" />
        <p class="font-sans text-[13px] text-muted-foreground">{{ t("logs.empty") }}</p>
        <p class="font-sans text-[11px] text-muted-foreground/75">{{ t("logs.emptyHint") }}</p>
      </div>

      <VirtualList
        v-else
        ref="list"
        :items="store.visible"
        :key-of="(e) => e.seq"
        :estimate="25"
        class="h-[min(58vh,520px)] font-mono text-[11.5px] leading-[1.55]"
        @at-bottom="onAtBottom"
      >
        <template #default="{ item: e }">
          <div class="flex items-start gap-2 border-b border-border/40 px-3 py-[3px] hover:bg-accent/40">
            <span class="w-[86px] shrink-0 text-muted-foreground/80">{{ shortTime(e.time) }}</span>
            <span class="w-[44px] shrink-0 font-semibold" :class="levelTone(e.level)">
              {{ levelLabel(e.level) }}
            </span>
            <span class="w-[76px] shrink-0 truncate text-muted-foreground" :title="e.source">
              {{ e.source }}
            </span>
            <span class="min-w-0 flex-1 whitespace-pre-wrap break-words">
              {{ e.message }}<span v-if="e.fields" class="text-muted-foreground"> {{ e.fields }}</span>
            </span>
          </div>
        </template>
      </VirtualList>
    </Card>

    <!-- 状态脚注 -->
    <div class="flex flex-wrap items-center gap-x-3 gap-y-1 px-1 text-[11px] text-muted-foreground">
      <span>{{ t("logs.count", { n: store.visible.length, total: store.entries.length }) }}</span>
      <span v-if="store.dropped > 0">{{ t("logs.dropped", { n: store.dropped }) }}</span>
      <Badge v-if="store.lost" variant="outline">{{ t("logs.lost") }}</Badge>
      <button
        v-if="store.filePath"
        class="ml-auto max-w-[420px] truncate font-mono text-muted-foreground/75 hover:text-foreground"
        :title="store.filePath"
        @click="copyAll"
      >
        {{ store.filePath }}
      </button>
    </div>

    <p v-if="store.fileError" class="text-[12px] text-amber-600 dark:text-amber-400">
      {{ t("logs.fileError", { err: store.fileError }) }}
    </p>
    <p v-if="store.error" class="text-[13px] text-destructive">{{ store.error }}</p>
  </PageShell>
</template>
