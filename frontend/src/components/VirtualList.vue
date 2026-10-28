<script setup lang="ts" generic="T extends object">
// 虚拟滚动列表：只把视口内的十几行挂到 DOM 上。
//
// 为什么自己做而不是引 vue-virtual-scroller：这个仓库只多一个依赖就多一份
// 体积与一次打包风险，而这里需要的只是「纵向、行高不一、只渲染视口」这一种
// 场景，全部实现就是一个前缀和 + 一次二分 + 一个 ResizeObserver。
//
// 行高不由调用方声明：日志消息会折行，行高天然不固定。所以这里「测到多少
// 记多少，没测过先按 estimate 估」。估算误差只影响滚动条的瞬时位置，用户
// 滚动时会被实测值立刻纠正，不会出现内容错位。
import { computed, nextTick, onBeforeUnmount, onBeforeUpdate, onMounted, onUpdated, ref } from "vue"
import { anchoredScrollTop, buildEdges, findRange, topIndex } from "@/lib/virtual"

const props = withDefaults(
  defineProps<{
    /** 要渲染的全部条目（可以上千条，DOM 里永远只有视口那几十行）。 */
    items: T[]
    /** 未测量过的行高估计值（px）。实测后会被覆盖。 */
    estimate?: number
    /** 视口上下各多渲染几行，快速滚动时不露白。 */
    overscan?: number
    /** 条目的稳定 key：按下标做 key 会在滚动时整段重建 DOM，动画和选中都会丢。 */
    keyOf: (item: T, index: number) => string | number
  }>(),
  { estimate: 22, overscan: 6 },
)

const emit = defineEmits<{ (e: "at-bottom", v: boolean): void }>()

const scroller = ref<HTMLElement | null>(null)
/** translateY 的容器，它的子元素就是当前渲染出来的行。 */
const content = ref<HTMLElement | null>(null)

const viewport = ref(0)
const scrollTop = ref(0)
/** 每次测量出新行高都自增，用来让 metrics 重算。 */
const version = ref(0)

/** 行高按「条目对象」记，而不是按下标：条目被裁剪或筛选后下标会变，对象不会。 */
const heights = new WeakMap<object, number>()

const metrics = computed(() => {
  void version.value
  const est = props.estimate
  return buildEdges(props.items.length, (i) => heights.get(props.items[i]) ?? est)
})

const range = computed(() =>
  findRange(metrics.value.edges, props.items.length, scrollTop.value, viewport.value, props.overscan),
)

const slice = computed(() => props.items.slice(range.value.start, range.value.end))

let rowObserver: ResizeObserver | null = null
let boxObserver: ResizeObserver | null = null

/**
 * 更新前的视口锚点。
 *
 * 日志缓冲满了会从头部裁掉旧条目，内容整体上移；不记锚点的话，用户正盯着
 * 的那一行会毫无征兆地跑掉。这里记住「首行是谁 + 视线在行内的偏移」，
 * 更新后把同样的位置找回。
 */
let anchor: { key: string | number; intra: number } | null = null

/** 把当前渲染出来的行读一遍高度；有变化就触发重算前缀和。 */
function measure() {
  const host = content.value
  if (!host) return
  const start = range.value.start
  let changed = false
  for (let i = 0; i < host.children.length; i++) {
    const el = host.children[i] as HTMLElement
    const item = props.items[start + i]
    if (!item) continue
    const h = el.offsetHeight
    if (h > 0 && heights.get(item) !== h) {
      heights.set(item, h)
      changed = true
    }
  }
  if (changed) version.value++
}

function watchRows() {
  const host = content.value
  if (!host) return
  // 重新登记：虚拟滚动每次切片区都会换掉一批 DOM 节点。
  rowObserver?.disconnect()
  for (const el of Array.from(host.children)) rowObserver?.observe(el)
  // 测量必须在登记之后立刻做一次，否则第一屏会按 estimate 排版，
  // 折行的行会把后面的内容整体顶偏。
  measure()
}

function onScroll() {
  const el = scroller.value
  if (!el) return
  scrollTop.value = el.scrollTop
  emit("at-bottom", isAtBottom())
}

function isAtBottom(): boolean {
  const el = scroller.value
  if (!el) return true
  return el.scrollHeight - el.scrollTop - el.clientHeight < 24
}

function scrollToBottom() {
  const el = scroller.value
  if (!el) return
  // 先测量再滚：新行刚落进 DOM 时 offsetHeight 才是准的，
  // 拿着旧的总高度去滚会停在「差几行」的位置。
  measure()
  el.scrollTop = el.scrollHeight
  scrollTop.value = el.scrollTop
  emit("at-bottom", true)
}

onMounted(() => {
  rowObserver = new ResizeObserver(() => measure())
  boxObserver = new ResizeObserver(() => {
    const el = scroller.value
    if (el) viewport.value = el.clientHeight
  })
  const el = scroller.value
  if (el) {
    viewport.value = el.clientHeight
    boxObserver.observe(el)
  }
  watchRows()
})

onBeforeUpdate(() => {
  const el = scroller.value
  if (!el) return
  // 贴在底部时不做锚点补偿：那时用户的意图是「跟着最新一行走」，
  // 补偿会把视口按回旧位置，反而像卡住了。
  if (isAtBottom()) {
    anchor = null
    return
  }
  const { edges } = metrics.value
  const idx = topIndex(edges, props.items.length, el.scrollTop, viewport.value)
  const item = props.items[idx]
  if (!item) return
  anchor = { key: props.keyOf(item, idx), intra: el.scrollTop - edges[idx] }
})

onUpdated(async () => {
  // 行内容（例如折行数量）变了，或者滚出了新的行，都要重新登记观察。
  await nextTick()
  watchRows()

  const a = anchor
  anchor = null
  if (!a) return
  const el = scroller.value
  if (!el) return
  const idx = props.items.findIndex((item, i) => props.keyOf(item, i) === a.key)
  // 锚点行本身被裁掉时不做补偿：此时往哪边都对不齐，保持原位更不突兀。
  if (idx < 0) return
  const next = anchoredScrollTop(metrics.value.edges, idx, a.intra)
  if (Math.abs(next - el.scrollTop) < 1) return
  el.scrollTop = next
  scrollTop.value = el.scrollTop
})

onBeforeUnmount(() => {
  rowObserver?.disconnect()
  boxObserver?.disconnect()
})

defineExpose({ scrollToBottom, isAtBottom })
</script>

<template>
  <div ref="scroller" class="relative overflow-auto" @scroll.passive="onScroll">
    <!-- 撑出完整滚动高度，真实行用 translateY 摆到对应位置 -->
    <div :style="{ height: metrics.total + 'px' }" class="relative w-full">
      <div ref="content" :style="{ transform: `translateY(${range.offset}px)` }">
        <div v-for="(item, i) in slice" :key="props.keyOf(item, range.start + i)">
          <slot :item="item" :index="range.start + i" />
        </div>
      </div>
    </div>
  </div>
</template>
