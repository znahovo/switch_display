// 全局状态。
//
// 单一 store 装全部状态（显示器 + 信号源 + 设置）：它们总是一起从后端
// State() 拉回来，拆成多个 store 只会增加同步代码。
import { defineStore } from "pinia"
import { computed, ref } from "vue"
import { api, toAppError, type AppError, type DisplayDTO, type PresetDTO, type SourceDTO, type StateDTO } from "@/api"

export const useAppStore = defineStore("app", () => {
  const displays = ref<DisplayDTO[]>([])
  const sources = ref<SourceDTO[]>([])
  const settings = ref<StateDTO["settings"]>({
    language: "",
    confirmAfterSwitch: true,
    defaultConfirmMs: 1200,
    hideToTrayOnClose: true,
  })
  const configPath = ref("")
  const warnings = ref<string[]>([])

  const loading = ref(false)
  const error = ref<AppError | null>(null)

  /** 正在切换的信号源 id，用于按钮 loading 态 */
  const switchingId = ref<string | null>(null)
  /** 最近一次切换结果，按 sourceId 记住（用于把「已确认」标在卡片上） */
  const lastOutcome = ref<Record<string, string>>({})

  const presets = ref<PresetDTO[]>([])

  /** 最近一次读到的当前输入口（按 displayId）。读不到就是空，
   *  这一栏是 best-effort：很多显示器不支持读回 0x60。 */
  const currentInputs = ref<Record<string, { vcp: number; value: number; alt: boolean }>>({})

  const displayById = computed(() => {
    const map = new Map<string, DisplayDTO>()
    for (const d of displays.value) map.set(d.id, d)
    return map
  })

  /** portLabel 把 (presetKey, value) 变成人类可读的端口名。 */
  function portLabel(src: SourceDTO): string {
    const p = presets.value.find(
      (x) => x.value === src.input.value && x.alt === src.input.alt,
    )
    if (p) return p.key.toUpperCase().replace("USBC", "USB-C").replace("HDMI", "HDMI ")
    return `${src.input.alt ? "ALT " : ""}${src.input.value}`
  }

  async function refresh() {
    loading.value = true
    error.value = null
    try {
      const state = await api.state()
      displays.value = state.displays ?? []
      sources.value = state.sources ?? []
      settings.value = state.settings
      configPath.value = state.configPath
      warnings.value = state.warnings ?? []
    } catch (e) {
      error.value = toAppError(e)
    } finally {
      loading.value = false
    }
  }

  async function refreshDisplays() {
    try {
      displays.value = await api.listDisplays()
    } catch (e) {
      error.value = toAppError(e)
    }
  }

  async function loadPresets() {
    try {
      presets.value = await api.presets()
    } catch {
      presets.value = []
    }
  }

  /** readCurrent 读一次某台显示器的当前输入口。 */
  async function readCurrent(displayId: string) {
    try {
      const input = await api.readCurrentInput(displayId)
      // value 为 0 或 vcp 不匹配说明没读到，不覆盖已知状态。
      if (input && input.vcp) {
        currentInputs.value = { ...currentInputs.value, [displayId]: input }
      }
    } catch {
      // 读不到是常态，静默失败。
    }
  }

  /** isCurrent 判断某源是否就是显示器当前所在的输入口。 */
  function isCurrent(src: SourceDTO): boolean {
    const cur = currentInputs.value[src.displayId]
    if (!cur) return false
    return cur.value === src.input.value && cur.alt === src.input.alt
  }

  async function switchTo(src: SourceDTO) {
    if (switchingId.value) return // 一次只允许一个切换：DDC 通道是独占的
    switchingId.value = src.id
    error.value = null
    try {
      const res = await api.switchSource(src.id)
      lastOutcome.value = { ...lastOutcome.value, [src.id]: res.outcome }
      // 切完再读一次，让「当前」徽标反映真实状态而不是猜测。
      void readCurrent(src.displayId)
    } catch (e) {
      error.value = toAppError(e)
    } finally {
      switchingId.value = null
    }
  }

  async function save(next: { sources: SourceDTO[]; settings: StateDTO["settings"] }) {
    try {
      const res = await api.saveConfig({
        displays: displays.value,
        sources: next.sources,
        settings: next.settings,
        configPath: configPath.value,
        warnings: [],
      })
      if (!res.ok) return { ok: false, warnings: res.warnings ?? [] }
      await refresh()
      return { ok: true, warnings: res.warnings ?? [] }
    } catch (e) {
      error.value = toAppError(e)
      return { ok: false, warnings: [], error: error.value }
    }
  }

  /** errorText 把后端 code 翻成当前语言的文案。 */
  function errorText(t: (key: string) => string): string {
    const e = error.value
    if (!e) return ""
    const key = `errors.${e.code}`
    const translated = t(key)
    return translated === key ? e.message : translated
  }

  return {
    displays,
    sources,
    settings,
    presets,
    currentInputs,
    configPath,
    warnings,
    loading,
    error,
    switchingId,
    lastOutcome,
    displayById,
    portLabel,
    readCurrent,
    isCurrent,
    refresh,
    refreshDisplays,
    loadPresets,
    switchTo,
    save,
    errorText,
  }
})
