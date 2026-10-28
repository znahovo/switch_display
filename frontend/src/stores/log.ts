// 日志页的状态：环形缓冲的增量镜像 + 筛选。
//
// 为什么自己维护一份数组而不是每次整表重取：
// 后端缓冲有 2000 条，Win 上子进程一次启动就能吐出几十行，
// 每 200ms 全量搬一次 JSON 会让界面在执行「滚动到底部」和「重渲染」之间
// 反复抖动。这里只追加新增的部分，并用 seq 作为 key 做稳定渲染。
import { defineStore } from "pinia"
import { computed, ref, shallowRef } from "vue"
import { logApi, type LogBatch, type LogEntryDTO } from "@/api"

/** 一次拉取的条数上限（与后端 logapp.MaxLimit 一致）。 */
const PAGE_SIZE = 2000

/** 界面最多保留的条数：再多用户也翻不动，且会让 DOM 变重。 */
const MAX_KEEP = 4000

export const useLogStore = defineStore("log", () => {
  /**
   * 全部日志（按 seq 升序）。
   *
   * 用 shallowRef 而不是 ref：日志条目是「写入即不变」的数据，没有任何字段
   * 会被原地改动，深层响应式在这里只有代价——几千条条目被遍历一次（筛选、
   * 复制、渲染）就要为每条造一个 Proxy。列表本身的替换（整体赋值）仍然会
   * 触发更新，这正是唯一需要响应的变化。
   */
  const entries = shallowRef<LogEntryDTO[]>([])
  /** 下次拉取的游标。 */
  const cursor = ref(0)
  const loading = ref(false)
  const error = ref("")
  const filePath = ref("")
  const fileError = ref("")
  const capacity = ref(0)
  const dropped = ref(0)
  /** 中间有日志因为缓冲淘汰而缺失（界面要提示，而不是假装连续）。 */
  const lost = ref(false)
  /** 活跃度筛选：空串表示全部。 */
  const level = ref("")
  /** 来源筛选：空串表示全部。 */
  const source = ref("")
  const sources = ref<string[]>([])
  /** 是否自动跟随最新一行。 */
  const follow = ref(true)

  /** 当前筛选下要显示的日志。 */
  const visible = computed(() => {
    const lv = level.value
    const src = source.value
    if (!lv && !src) return entries.value
    return entries.value.filter(
      (e) => (!lv || e.level === lv) && (!src || e.source === src),
    )
  })

  const hasError = computed(() => entries.value.some((e) => e.level === "error"))

  /** 应用一批增量。 */
  function absorb(batch: LogBatch) {
    if (batch.entries.length > 0) {
      entries.value = [...entries.value, ...batch.entries]
      // 来源是从日志里长出来的，不是预设的：deskflow-core 只有在用户
      // 真的开始共享之后才会开口，那时筛选条得自动多出一个标签。
      const known = new Set(sources.value)
      for (const e of batch.entries) {
        if (e.source && !known.has(e.source)) {
          known.add(e.source)
          sources.value = [...sources.value, e.source]
        }
      }
      // 只留尾部：日志页是「看最近发生了什么」，不是归档。
      if (entries.value.length > MAX_KEEP) {
        entries.value = entries.value.slice(entries.value.length - MAX_KEEP)
      }
    }
    // 游标永远跟着后端走，即使本次没取到条目（可能被 limit 截断）。
    if (batch.latest > cursor.value) cursor.value = batch.latest
    capacity.value = batch.capacity
    dropped.value = batch.dropped
    filePath.value = batch.filePath
    fileError.value = batch.fileError ?? ""
    if (batch.lost) lost.value = true
  }

  /**
   * 拉取增量。
   *
   * reset 为 true 时（首次进入页面 / 点刷新）从 0 重新拉，
   * 因为中途打开页面时后端的 seq 早已不是从 1 开始。
   */
  async function refresh(reset = false) {
    if (loading.value) return
    loading.value = true
    error.value = ""
    try {
      if (reset) {
        entries.value = []
        cursor.value = 0
        lost.value = false
        // 来源列表要跟着重取：清空后旧来源已经不存在了，
        // 留着它们只会得到一堆筛不出东西的标签。
        sources.value = []
      }
      const batch = await logApi.query(cursor.value, PAGE_SIZE)
      absorb(batch)
      // 首次（或清空后）用后端列表补一次：游标从 0 开始时
      // 历史上出现过的来源也要能筛。
      if (sources.value.length === 0) {
        sources.value = (await logApi.sources()) ?? []
      }
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  /** 清空（内存缓冲与界面一起清，磁盘文件保留）。 */
  async function clear() {
    try {
      await logApi.clear()
      entries.value = []
      cursor.value = 0
      lost.value = false
      await refresh(true)
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  /** 设置级别筛选。 */
  function setLevel(v: string) {
    level.value = level.value === v ? "" : v
  }

  /** 设置来源筛选。 */
  function setSource(v: string) {
    source.value = source.value === v ? "" : v
  }

  /** 复制全部可见日志到剪贴板（用户反馈问题时最常用的一步）。 */
  async function copyAll(): Promise<boolean> {
    const text = visible.value
      .map((e) => `${e.time} ${e.level.toUpperCase().padEnd(5)} [${e.source}] ${e.message}${e.fields ? " " + e.fields : ""}`)
      .join("\n")
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      return false
    }
  }

  return {
    entries,
    cursor,
    loading,
    error,
    filePath,
    fileError,
    capacity,
    dropped,
    lost,
    level,
    source,
    sources,
    follow,
    visible,
    hasError,
    refresh,
    clear,
    setLevel,
    setSource,
    copyAll,
  }
})
