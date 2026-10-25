// 前端与 Go 的唯一通道。
//
// 所有 Wails 绑定调用都收口到这里，而不是让组件各自 import bindings：
// 1) 组件里出现 $Call.ByID 之类的实现细节是泄漏抽象；
// 2) 需要 mock（例如浏览器里跑 vite dev 不带 Go 后端）时只改一处。
import * as DisplayService from "@bindings/github.com/znahovo/switch-display/internal/presentation/wailsservice/displayservice.js"
import type {
  DisplayDTO,
  InputDTO,
  PresetDTO,
  SaveResultDTO,
  SourceDTO,
  StateDTO,
  SwitchResultDTO,
} from "@bindings/github.com/znahovo/switch-display/internal/application/dto/models.js"
import { Events } from "@wailsio/runtime"

export type {
  DisplayDTO,
  InputDTO,
  PresetDTO,
  SaveResultDTO,
  SourceDTO,
  StateDTO,
  SwitchResultDTO,
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

export const api = {
  state: (): Promise<StateDTO> => DisplayService.State(),
  listDisplays: (): Promise<DisplayDTO[]> => DisplayService.ListDisplays(),
  presets: (): Promise<PresetDTO[]> => DisplayService.Presets(),
  altPresets: (): Promise<PresetDTO[]> => DisplayService.AltPresets(),
  switchSource: (id: string): Promise<SwitchResultDTO> => DisplayService.SwitchSource(id),
  readCurrentInput: (displayId: string): Promise<InputDTO> =>
    DisplayService.ReadCurrentInput(displayId),
  saveConfig: (state: StateDTO): Promise<SaveResultDTO> => DisplayService.SaveConfig(state),
  exportConfig: (dest: string): Promise<void> => DisplayService.ExportConfig(dest),
  importConfig: (src: string): Promise<StateDTO> => DisplayService.ImportConfig(src),
  configPath: (): Promise<string> => DisplayService.ConfigPath(),
}

/** 订阅后端事件；返回取消订阅函数。 */
export function onEvent<T>(name: string, fn: (payload: T) => void): () => void {
  return Events.On(name, (event: { data: T }) => fn(event.data))
}
