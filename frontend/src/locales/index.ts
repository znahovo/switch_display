// i18n 骨架。
//
// 语言来源优先级：配置里保存的 language > 系统语言 > 英文。
// 之所以保留 vue-i18n 而不是自己写个 t()：插值、复数、fallback 都是现成的，
// 而后端只负责返回 code，文案全部留在前端。
import { createI18n } from "vue-i18n"
import en from "./en"
import zhCN from "./zh-CN"

export const SUPPORTED = ["en", "zh-CN"] as const
export type Locale = (typeof SUPPORTED)[number]

/** detectLocale 把系统语言收敛到支持列表里的一个。 */
export function detectLocale(preferred?: string): Locale {
  if (preferred && (SUPPORTED as readonly string[]).includes(preferred)) {
    return preferred as Locale
  }
  const nav = typeof navigator !== "undefined" ? navigator.language : "en"
  if (nav.toLowerCase().startsWith("zh")) return "zh-CN"
  return "en"
}

export const i18n = createI18n({
  legacy: false,
  locale: detectLocale(),
  fallbackLocale: "en",
  messages: { en, "zh-CN": zhCN },
})

export function setLocale(locale: Locale) {
  i18n.global.locale.value = locale
}
