// 当前运行平台。Wails 在启动时把 `window._wails.environment` 注入到页面里
// （见 wails 的 runtime_prod.go），所以这里同步就能读到，不需要 await。
//
// 为什么不直接用 navigator.platform / userAgent：WebView2 和 WKWebView 的 UA
// 都能被改，而且它回答的是「浏览器像什么」而不是「我们在哪个系统上」。
// Wails 注入的 OS 就是 Go 的 GOOS，是唯一可靠的来源。

/** 在浏览器里跑 vite dev（没有 Wails 注入）时，退回 UA 猜测，方便调试。 */
function detectOS(): string {
  const injected = (window as unknown as { _wails?: { environment?: { OS?: string } } })._wails
    ?.environment?.OS
  if (injected) return injected

  const ua = navigator.userAgent ?? ""
  if (/windows/i.test(ua)) return "windows"
  if (/mac/i.test(ua)) return "darwin"
  if (/linux/i.test(ua)) return "linux"
  return "unknown"
}

export const os = detectOS()

/** macOS：自定义了隐藏标题栏 + 红绿灯，需要自己画拖动区。 */
export const isMac = os === "darwin"
export const isWindows = os === "windows"

/**
 * 录制快捷键时提示用户「必须有修饰键」的键名。
 *
 * macOS 上显示符号（与菜单、系统习惯一致），其它平台显示单词：
 * Windows 用户看到 "⌘⌥⌃⇧" 是不知道要按哪几个键的。
 */
export const modifierHintKeys = isMac ? "⌘ ⌥ ⌃ ⇧" : "Ctrl Alt Shift"
