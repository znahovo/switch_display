// 把键盘事件转成 Wails 的 accelerator 字符串（形如 "Cmd+Option+1"）。
//
// 为什么必须用 event.code 而不是 event.key：在 macOS 上按住 Option 时
// event.key 会变成特殊字符（Option+1 → "¡"），如果拿它拼快捷键，
// 用户录下来的东西和实际要按的键就完全对不上了。
//
// accelerator 的字符串本身是跨平台通用的：Wails 在 Windows 上把
// Cmd/CmdOrCtrl 都解析成 Ctrl、Option 解析成 Alt（见 keys.go 的 modifierMap
// 与 global_shortcut_windows.go）。所以只有「录制时写什么词」和
// 「显示成什么样」需要按平台区分，存盘格式不用变。
import { isMac } from "./platform"

/** 修饰键在 accelerator 里的顺序，与 Wails 各平台实现的输出顺序一致。 */
function modifierOrder(): readonly string[] {
  return isMac ? (["Shift", "Ctrl", "Option", "Cmd"] as const) : (["Shift", "Ctrl", "Alt", "Super"] as const)
}

/** 单独按下时事件里的 e.key 值：这些键不能自己构成一个快捷键。 */
const MODIFIER_KEYS = ["Shift", "Control", "Alt", "Meta", "AltGraph", "OS", "Hyper", "Super"]

/** codeToKey 把 KeyboardEvent.code 映射成 Wails 认识的键名。 */
function codeToKey(code: string): string | null {
  // 字母 / 数字
  const letter = /^Key([A-Z])$/.exec(code)
  if (letter) return letter[1].toLowerCase()

  const digit = /^Digit([0-9])$/.exec(code)
  if (digit) return digit[1]

  const fn = /^F([0-9]{1,2})$/.exec(code)
  if (fn) {
    const n = Number(fn[1])
    // Wails 的 namedKeys 只到 f35，够用。
    return n >= 1 && n <= 35 ? `f${n}` : null
  }

  // 具名键：名字必须与 Wails keys.go 的 namedKeys 完全一致。
  const named: Record<string, string> = {
    Space: "space",
    Enter: "enter",
    NumpadEnter: "enter",
    Escape: "escape",
    Tab: "tab",
    Backspace: "backspace",
    Delete: "delete",
    ArrowLeft: "left",
    ArrowRight: "right",
    ArrowUp: "up",
    ArrowDown: "down",
    Home: "home",
    End: "end",
    PageUp: "page up",
    PageDown: "page down",
    Minus: "-",
    Equal: "=",
    BracketLeft: "[",
    BracketRight: "]",
    Backslash: "\\",
    Semicolon: ";",
    Quote: "'",
    Comma: ",",
    Period: ".",
    Slash: "/",
    Backquote: "`",
  }
  if (code in named) {
    // accelerator 用 "+" 当分隔符，所以按键本身就是 "+" 时要写成 "plus"。
    return named[code] === "+" ? "plus" : named[code]
  }
  return null
}

/** 判断事件里的修饰键，返回按固定顺序排列的名字。 */
function modifiersOf(e: KeyboardEvent): string[] {
  const out: string[] = []
  // 在 macOS 上 Option+字母 会让 metaKey 误报，所以用 getModifierState 更稳。
  if (e.shiftKey) out.push("Shift")
  if (e.ctrlKey) out.push("Ctrl")
  // Windows / Linux 的 Meta 是 Win 键，必须写成 Super，否则会被当成 Ctrl。
  if (e.altKey) out.push(isMac ? "Option" : "Alt")
  if (e.metaKey) out.push(isMac ? "Cmd" : "Super")
  return out
}

/**
 * eventToAccelerator 把一次按键转成 accelerator。
 *
 * 返回 null 表示「这次按键还不能构成快捷键」：只按了修饰键，
 * 或者没有修饰键（裸字母会抢走所有输入框的按键，必须禁止）。
 */
export function eventToAccelerator(e: KeyboardEvent): string | null {
  if (isModifierOnly(e)) return null

  const mods = modifiersOf(e)
  if (mods.length === 0) return null

  const key = codeToKey(e.code)
  if (!key) return null

  // 按平台顺序排，保证同一个快捷键录两次得到同一个字符串（配置 diff 才稳定）。
  const order = modifierOrder()
  mods.sort((a, b) => order.indexOf(a) - order.indexOf(b))

  return [...mods, key].join("+")
}

/** isModifierOnly 判断这次按键是否只是按了修饰键（用于「还在录」的提示）。 */
export function isModifierOnly(e: KeyboardEvent): boolean {
  return MODIFIER_KEYS.includes(e.key)
}

/** 键名显示：单字符大写，多词的每个词首字母大写（"page up" → "Page Up"）。 */
function keyLabel(key: string): string {
  return key.length === 1 ? key.toUpperCase() : key.replace(/\b\w/g, (c) => c.toUpperCase())
}

// macOS 用符号（与系统菜单一致）；其它平台用单词，并按 "Ctrl+Alt+1" 拼。
const MAC_SYMBOLS: Record<string, string> = {
  cmd: "⌘",
  command: "⌘",
  cmdorctrl: "⌘",
  ctrl: "⌃",
  control: "⌃",
  option: "⌥",
  alt: "⌥",
  shift: "⇧",
  super: "⌘",
}

const WINDOWS_NAMES: Record<string, string> = {
  cmd: "Ctrl",
  command: "Ctrl",
  cmdorctrl: "Ctrl",
  ctrl: "Ctrl",
  control: "Ctrl",
  alt: "Alt",
  option: "Alt",
  shift: "Shift",
  super: "Win",
}

/**
 * formatAccelerator 把 accelerator 变成给人看的文本。
 *
 * macOS：`Cmd+Option+1` → `⌘⌥1`；其它平台：`Ctrl+Alt+1`。
 */
export function formatAccelerator(accel: string): string {
  if (!accel) return ""
  const parts = accel.split("+")
  // 键名可能是 "page up" 这种带空格的，split("+") 不受影响。
  const key = parts.pop() ?? ""

  if (isMac) {
    const mods = parts.map((p) => MAC_SYMBOLS[p.toLowerCase()] ?? p).join("")
    return mods + keyLabel(key)
  }

  const mods = parts.map((p) => WINDOWS_NAMES[p.toLowerCase()] ?? p)
  return [...mods, keyLabel(key)].join("+")
}
