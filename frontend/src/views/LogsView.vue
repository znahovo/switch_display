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
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue"
import { useI18n } from "vue-i18n"
import { ArrowDownToLine, Check, Copy, Eraser, Pause, Play, RefreshCw, ScrollText } from "lucide-vue-next"
import PageShell from "@/components/PageShell.vue"
import Badge from "@/components/ui/Badge.vue"
import Button from "@/components/ui/Button.vue"
import Card from "@/components/ui/Card.vue"
import Checkbox from "@/components/ui/Checkbox.vue"
import { EventName, onEvent } from "@/api"
import { useLogStore } from "@/stores/log"

const { t } = useI18n()
const store = useLogStore()

const scroller = ref<HTMLElement | null>(null)
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

/** 跟随：新内容进来自动滚到底。 */
watch(
  () => store.visible.length,
  async () => {
    if (!store.follow) return
    await nextTick()
    const el = scroller.value
    if (el) el.scrollTop = el.scrollHeight
  },
)

/** 用户手动往上滚就取消跟随——正在读报错时被拽到底部最烦人。 */
function onScroll() {
  const el = scroller.value
  if (!el) return
  const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 24
  store.follow = atBottom
}

async function copyAll() {
  if (await store.copyAll()) {
    copied.value = true
    setTimeout(() => (copied.value = false), 1600)
  }
}

async function scrollToBottom() {
  store.follow = true
  await nextTick()
  const el = scroller.value
  if (el) el.scrollTop = el.scrollHeight
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

    <!-- 日志正文 -->
    <Card class="overflow-hidden p-0">
      <div
        ref="scroller"
        class="h-[min(58vh,520px)] overflow-auto font-mono text-[11.5px] leading-[1.55]"
        @scroll.passive="onScroll"
      >
        <div v-if="!store.visible.length" class="flex flex-col items-center gap-2 px-6 py-16 text-center">
          <ScrollText class="size-5 text-muted-foreground" />
          <p class="font-sans text-[13px] text-muted-foreground">{{ t("logs.empty") }}</p>
          <p class="font-sans text-[11px] text-muted-foreground/75">{{ t("logs.emptyHint") }}</p>
        </div>

        <table v-else class="w-full border-collapse">
          <tbody>
            <tr
              v-for="e in store.visible"
              :key="e.seq"
              class="border-b border-border/40 last:border-0 hover:bg-accent/40"
            >
              <td class="w-[86px] whitespace-nowrap px-3 py-0.5 align-top text-muted-foreground/80">
                {{ shortTime(e.time) }}
              </td>
              <td class="w-[54px] whitespace-nowrap px-1 py-0.5 align-top font-semibold" :class="levelTone(e.level)">
                {{ levelLabel(e.level) }}
              </td>
              <td class="w-[76px] truncate px-1 py-0.5 align-top text-muted-foreground" :title="e.source">
                {{ e.source }}
              </td>
              <td class="whitespace-pre-wrap break-words px-3 py-0.5 align-top">
                {{ e.message }}<span v-if="e.fields" class="text-muted-foreground"> {{ e.fields }}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
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
