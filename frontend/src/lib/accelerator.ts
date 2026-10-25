// 把键盘事件转成 Wails 的 accelerator 字符串（形如 "Cmd+Option+1"）。
//
// 为什么必须用 event.code 而不是 event.key：在 macOS 上按住 Option 时
// event.key 会变成特殊字符（Option+1 → "¡"），如果拿它拼快捷键，
// 用户录下来的东西和实际要按的键就完全对不上了。

/** 允许的修饰键顺序，与 Wails accelerator_darwin.go 的输出顺序一致。 */
const MODIFIER_ORDER = ["Shift", "Ctrl", "Option", "Cmd"] as const

type Modifier = (typeof MODIFIER_ORDER)[number]

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
function modifiersOf(e: KeyboardEvent): Modifier[] {
  const out: Modifier[] = []
  // 在 macOS 上 Option+字母 会让 metaKey 误报，所以用 getModifierState 更稳。
  if (e.shiftKey) out.push("Shift")
  if (e.ctrlKey) out.push("Ctrl")
  if (e.altKey) out.push("Option")
  if (e.metaKey) out.push("Cmd")
  return out
}

/**
 * eventToAccelerator 把一次按键转成 accelerator。
 *
 * 返回 null 表示「这次按键还不能构成快捷键」：只按了修饰键，
 * 或者没有修饰键（裸字母会抢走所有输入框的按键，必须禁止）。
 */
export function eventToAccelerator(e: KeyboardEvent): string | null {
  if (MODIFIER_ORDER.includes(e.key as Modifier)) return null

  const mods = modifiersOf(e)
  if (mods.length === 0) return null

  const key = codeToKey(e.code)
  if (!key) return null

  return [...mods, key].join("+")
}

/** isModifierOnly 判断这次按键是否只是按了修饰键（用于「还在录」的提示）。 */
export function isModifierOnly(e: KeyboardEvent): boolean {
  return MODIFIER_ORDER.includes(e.key as Modifier)
}

/** formatAccelerator 把 accelerator 变成 macOS 风格的显示文本 "⌘⌥1"。 */
export function formatAccelerator(accel: string): string {
  if (!accel) return ""
  const parts = accel.split("+")
  const symbols: Record<string, string> = {
    cmd: "⌘",
    command: "⌘",
    cmdorctrl: "⌘",
    ctrl: "⌃",
    control: "⌃",
    option: "⌥",
    alt: "⌥",
    shift: "⇧",
  }
  // 键名可能是 "page up" 这种带空格的，split("+") 不受影响。
  const key = parts.pop() ?? ""
  const mods = parts.map((p) => symbols[p.toLowerCase()] ?? p).join("")
  const keyLabel = key.length === 1 ? key.toUpperCase() : key.replace(/\b\w/g, (c) => c.toUpperCase())
  return mods + keyLabel
}
