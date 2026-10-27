// 前端与 Go 的唯一通道。
//
// 所有 Wails 绑定调用都收口到这里，而不是让组件各自 import bindings：
// 1) 组件里出现 $Call.ByID 之类的实现细节是泄漏抽象；
// 2) 需要 mock（例如浏览器里跑 vite dev 不带 Go 后端）时只改一处。
import * as DisplayService from "@bindings/github.com/znahovo/switch-display/internal/presentation/wailsservice/displayservice.js"
import * as LinkService from "@bindings/github.com/znahovo/switch-display/internal/presentation/wailsservice/linkservice.js"
import type {
  CoreStatusDTO,
  DisplayDTO,
  InputDTO,
  LinkSelfDTO,
  LinkStateDTO,
  PairResultDTO,
  PeerDTO,
  PresetDTO,
  SaveResultDTO,
  SourceDTO,
  StateDTO,
  SwitchResultDTO,
  TrustedPeerDTO,
} from "@bindings/github.com/znahovo/switch-display/internal/application/dto/models.js"
import { Events } from "@wailsio/runtime"

export type {
  CoreStatusDTO,
  DisplayDTO,
  InputDTO,
  LinkSelfDTO,
  LinkStateDTO,
  PairResultDTO,
  PeerDTO,
  PresetDTO,
  SaveResultDTO,
  SourceDTO,
  StateDTO,
  SwitchResultDTO,
  TrustedPeerDTO,
}

/** 后端事件名，必须与 internal/application/port.go 的常量一致。 */
export const EventName = {
  switchStarted: "switch:started",
  switchFinished: "switch:finished",
  stateChanged: "state:changed",
  /** 托盘「设置…」被点击：前端要跳到设置页 */
  openSettings: "dialogs:open-settings",
  /** 后端要求前端切到某个页面，payload 是路由路径（如 "/settings"） */
  showView: "ui:show-view",
  /** 共享状态 / 对端列表 / core 状态变化：前端重拉 LinkService.State */
  linkChanged: "link:changed",
} as const

/** 后端返回的结构化错误（见 internal/presentation/wailsservice/errors.go）。 */
export interface AppError {
  code: string
  op: string
  message: string
}

/**
 * 把后端抛出的异常规整成 AppError。
 *
 * Wails 传回来的错误是字符串化的 JSON，所以这里要尝试解析；
 * 解析失败就当成 internal 错误，至少不丢信息。
 */
export function toAppError(err: unknown): AppError {
  const fallback: AppError = { code: "internal", op: "", message: String(err) }
  if (typeof err === "string") {
    try {
      const parsed = JSON.parse(err)
      if (parsed && typeof parsed.code === "string") return parsed as AppError
    } catch {
      return { ...fallback, message: err }
    }
    return { ...fallback, message: err }
  }
  if (err instanceof Error) return { ...fallback, message: err.message }
  return fallback
}

// 绑定生成器把 Go 的切片返回值标成 `T[] | null`（nil 切片 → null）。
// 前端不希望到处写 `?? []`，所以在这一层收口：这个文件本来就是唯一通道，
// 把「Go 的 nil」翻译成「空数组」属于它该干的适配。
export const api = {
  state: (): Promise<StateDTO> => DisplayService.State(),
  listDisplays: async (): Promise<DisplayDTO[]> => (await DisplayService.ListDisplays()) ?? [],
  presets: async (): Promise<PresetDTO[]> => (await DisplayService.Presets()) ?? [],
  altPresets: async (): Promise<PresetDTO[]> => (await DisplayService.AltPresets()) ?? [],
  switchSource: (id: string): Promise<SwitchResultDTO> => DisplayService.SwitchSource(id),
  readCurrentInput: (displayId: string): Promise<InputDTO> =>
    DisplayService.ReadCurrentInput(displayId),
  saveConfig: (state: StateDTO): Promise<SaveResultDTO> => DisplayService.SaveConfig(state),
  exportConfig: (dest: string): Promise<void> => DisplayService.ExportConfig(dest),
  importConfig: (src: string): Promise<StateDTO> => DisplayService.ImportConfig(src),
  configPath: (): Promise<string> => DisplayService.ConfigPath(),
}

/**
 * LinkState 是收口后的共享状态：绑定把 nil 切片标成 null，
 * 这里统一成空数组，页面就不用到处写 `?? []`。
 */
export type LinkState = Omit<LinkStateDTO, "peers" | "trusted" | "core"> & {
  peers: PeerDTO[]
  trusted: TrustedPeerDTO[]
  core: Omit<CoreStatusDTO, "logTail"> & { logTail: string[] }
}

/**
 * 局域网共享（Deskflow）的 API。
 *
 * 与 api 并列而不是塞进去：两者后端服务不同，前端也分开订阅事件，
 * 混在一起会让「显示器热插拔导致共享页重渲染」这种耦合悄悄出现。
 */
export const linkApi = {
  state: async (): Promise<LinkState> => {
    const state = await LinkService.State()
    return {
      ...state,
      peers: state.peers ?? [],
      trusted: state.trusted ?? [],
      core: { ...state.core, logTail: state.core.logTail ?? [] },
    }
  },
  setDeviceName: (name: string): Promise<PairResultDTO> => LinkService.SetDeviceName(name),
  hostWith: (peerId: string): Promise<PairResultDTO> => LinkService.HostWith(peerId),
  joinHost: (peerId: string): Promise<PairResultDTO> => LinkService.JoinHost(peerId),
  stop: (): Promise<PairResultDTO> => LinkService.Stop(),
  unpair: (peerId: string): Promise<PairResultDTO> => LinkService.Unpair(peerId),
  refreshDeskflow: (): Promise<boolean> => LinkService.RefreshDeskflow(),
}

/** 订阅后端事件；返回取消订阅函数。 */
export function onEvent<T>(name: string, fn: (payload: T) => void): () => void {
  return Events.On(name, (event: { data: T }) => fn(event.data))
}
