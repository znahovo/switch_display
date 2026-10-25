// 主题跟随系统。
//
// 窗口用了 MacBackdropTranslucent，背景必须与系统外观一致，否则会出现
// 「浅色网页 + 深色红绿灯」的割裂感。所以这里不去读配置，直接跟
// prefers-color-scheme 走（Wails 的 mac 窗口本身也会跟着系统变）。
const query = "(prefers-color-scheme: dark)"

function apply(dark: boolean) {
  document.documentElement.classList.toggle("dark", dark)
  // 让原生控件（滚动条、下拉箭头）也用对应外观
  document.documentElement.style.colorScheme = dark ? "dark" : "light"
}

/** watchTheme 立刻应用一次，并订阅系统外观变化；返回取消订阅函数。 */
export function watchTheme(): () => void {
  if (typeof window === "undefined" || !window.matchMedia) {
    apply(false)
    return () => {}
  }
  const mq = window.matchMedia(query)
  apply(mq.matches)
  const onChange = (e: MediaQueryListEvent) => apply(e.matches)
  mq.addEventListener("change", onChange)
  return () => mq.removeEventListener("change", onChange)
}
