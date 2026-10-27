// 局域网共享（Deskflow）的前端状态。
//
// 单独一个 store 而不是塞进 app.ts：共享状态由 link:changed 驱动、
// 每 2 秒可能刷新一次，和「显示器/信号源」的刷新节奏完全不同。
// 混在一起会让切换台跟着一起重渲染。
import { defineStore } from "pinia"
import { computed, ref } from "vue"
import {
  linkApi,
  toAppError,
  type AppError,
  type LinkState,
  type PeerDTO,
} from "@/api"

/** 空状态：首帧（后端还没起来）用它占位，避免模板里到处判空。 */
function emptyState(): LinkState {
  return {
    self: { id: "", name: "", role: "idle", fingerprint: "" },
    peers: [],
    trusted: [],
    core: { state: "stopped", role: "idle", pid: 0, message: "", logTail: [] },
    deskflowAvailable: false,
    deskflowBinary: "",
    deskflowDir: "",
    configPath: "",
    discoverPort: 0,
    lastHostId: "",
    error: "",
  }
}

export const useLinkStore = defineStore("link", () => {
  const state = ref<LinkState>(emptyState())

  const loading = ref(false)
  const error = ref<AppError | null>(null)

  /** 正在配对的设备 id，用于把按钮变成 loading 态 */
  const pairingId = ref<string | null>(null)
  /** 最近一次操作的结果文案（成功/失败都可能是它） */
  const notice = ref("")

  const self = computed(() => state.value.self)
  const peers = computed(() => state.value.peers)
  const trusted = computed(() => state.value.trusted)
  const core = computed(() => state.value.core)

  /** running 表示本机正在共享（无论主机还是客机）。 */
  const running = computed(() => core.value.state === "running")
  /** role 是本机当前角色：idle / server / client。 */
  const role = computed(() => self.value.role)

  /** 在线且尚未配对的设备：这是「可以点的主机候选」。 */
  const availablePeers = computed(() =>
    peers.value.filter((p) => p.reachable && !p.paired),
  )

  async function refresh() {
    loading.value = true
    error.value = null
    try {
      state.value = await linkApi.state()
    } catch (e) {
      error.value = toAppError(e)
    } finally {
      loading.value = false
    }
  }

  /** rename 修改本机展示名。 */
  async function rename(name: string) {
    notice.value = ""
    try {
      const res = await linkApi.setDeviceName(name)
      notice.value = res.message
      if (res.ok) await refresh()
      return res.ok
    } catch (e) {
      error.value = toAppError(e)
      return false
    }
  }

  /** hostWith 以本机作为主机邀请对方。 */
  async function hostWith(peer: PeerDTO) {
    if (pairingId.value) return
    pairingId.value = peer.id
    error.value = null
    notice.value = ""
    try {
      const res = await linkApi.hostWith(peer.id)
      notice.value = res.message
    } catch (e) {
      error.value = toAppError(e)
    } finally {
      pairingId.value = null
    }
  }

  /** joinHost 以本机作为客机加入对方。 */
  async function joinHost(peer: PeerDTO) {
    if (pairingId.value) return
    pairingId.value = peer.id
    error.value = null
    notice.value = ""
    try {
      const res = await linkApi.joinHost(peer.id)
      notice.value = res.message
    } catch (e) {
      error.value = toAppError(e)
    } finally {
      pairingId.value = null
    }
  }

  async function stop() {
    error.value = null
    notice.value = ""
    try {
      const res = await linkApi.stop()
      notice.value = res.message
      await refresh()
    } catch (e) {
      error.value = toAppError(e)
    }
  }

  async function unpair(id: string) {
    error.value = null
    notice.value = ""
    try {
      const res = await linkApi.unpair(id)
      notice.value = res.message
      await refresh()
    } catch (e) {
      error.value = toAppError(e)
    }
  }

  /** rescanDeskflow 重新查找 deskflow-core（用户刚装完）。 */
  async function rescanDeskflow() {
    try {
      const ok = await linkApi.refreshDeskflow()
      notice.value = ok ? "" : "not-found"
      await refresh()
      return ok
    } catch (e) {
      error.value = toAppError(e)
      return false
    }
  }

  /** noticeText 把后端返回的稳定标识翻成当前语言的文案。 */
  function noticeText(t: (key: string) => string): string {
    const code = notice.value
    if (!code) return ""
    // "not-found" 由视图单独渲染（它要带一段说明和按钮）。
    if (code === "not-found") return ""
    const key = `share.notice.${code}`
    const translated = t(key)
    // 没翻译（后端加了新标识但前端还没跟上）就原样显示，别显示 key。
    return translated === key ? code : translated
  }

  /**
   * stateErrorText 翻译后端状态里的 error。
   *
   * 后端只会产出两类：`pair_rejected:<reason>`（对端拒绝，reason 是稳定
   * 标识）和底层错误原文（socket、文件系统等，只能原样显示）。
   */
  function stateErrorText(t: (key: string) => string): string {
    const raw = state.value.error
    if (!raw) return ""
    const prefix = "pair_rejected:"
    if (!raw.startsWith(prefix)) return raw
    const reason = raw.slice(prefix.length) || "unknown"
    const key = `share.reject.${reason}`
    const translated = t(key)
    // 后端加了新原因码而前端还没跟上时，退回通用文案而不是显示 key。
    return translated === key ? t("share.reject.unknown") : translated
  }

  /**
   * coreMsgText 翻译 core 状态里的 message。
   *
   * 后端自己产生的那几条是稳定标识（client_started / exit_config 等），
   * 由前端翻译；Deskflow 自己吐的 ERROR 行是英文原文，翻不出来就原样显示。
   */
  function coreMsgText(t: (key: string) => string): string {
    const raw = core.value.message
    if (!raw) return ""
    const key = `share.coreMsg.${raw}`
    const translated = t(key)
    return translated === key ? raw : translated
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
    state,
    loading,
    error,
    pairingId,
    notice,
    self,
    peers,
    trusted,
    core,
    running,
    role,
    availablePeers,
    refresh,
    rename,
    hostWith,
    joinHost,
    stop,
    unpair,
    rescanDeskflow,
    errorText,
    stateErrorText,
    coreMsgText,
    noticeText,
  }
})
