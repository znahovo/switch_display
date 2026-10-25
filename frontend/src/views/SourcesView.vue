<script setup lang="ts">
// 信号源管理页：增删改 + 快捷键录制。
//
// 编辑用「草稿数组」而不是逐条保存到后端：端口唯一性、快捷键唯一性都是
// 跨条目的校验（见 domain/source/repository.go 的 Validate），必须整体提交
// 才能给出准确的错误提示。
import { computed, onMounted, onUnmounted, ref, watch } from "vue"
import { useI18n } from "vue-i18n"
import { Keyboard, Plus, Trash2, Undo2 } from "lucide-vue-next"
import { useAppStore } from "@/stores/app"
import Button from "@/components/ui/Button.vue"
import Card from "@/components/ui/Card.vue"
import Input from "@/components/ui/Input.vue"
import Label from "@/components/ui/Label.vue"
import Select from "@/components/ui/Select.vue"
import { eventToAccelerator, formatAccelerator, isModifierOnly } from "@/lib/accelerator"
import type { SourceDTO } from "@/api"

const { t } = useI18n()
const store = useAppStore()

/** 草稿：进入页面时从 store 拷一份，编辑期间不污染全局状态。 */
const draft = ref<SourceDTO[]>([])
const settingsDraft = ref(store.settings)
const dirty = ref(false)
const saving = ref(false)
const formError = ref("")

/** 正在录制快捷键的信号源 id（null = 没在录）。 */
const recording = ref<string | null>(null)
const recordHint = ref("")

const portOptions = computed(() => [
  ...store.presets.map((p) => ({
    key: p.key,
    label: prettyKey(p.key),
    value: p.value,
    alt: p.alt,
  })),
  { key: "custom", label: t("common.custom"), value: -1, alt: false },
])

onMounted(async () => {
  await Promise.all([store.loadPresets(), store.refresh()])
  reset()
})

function reset() {
  draft.value = store.sources.map((s) => ({ ...s, input: { ...s.input } }))
  settingsDraft.value = { ...store.settings }
  dirty.value = false
  formError.value = ""
}

function prettyKey(key: string): string {
  return key.toUpperCase().replace("USBC", "USB-C").replace("HDMI", "HDMI ").replace("ALT ", "")
}

function markDirty() {
  dirty.value = true
  formError.value = ""
}

/** 当前行选中的端口 key（从已有的 input 反查）。 */
function selectedKey(src: SourceDTO): string {
  const p = store.presets.find((x) => x.value === src.input.value && x.alt === src.input.alt)
  return p?.key ?? "custom"
}

function onPortChange(src: SourceDTO, key: string) {
  const opt = portOptions.value.find((p) => p.key === key)
  if (!opt || opt.value < 0) return // 选「自定义」时保留原值，让用户手填
  src.input = {
    vcp: opt.alt ? 0xf4 : 0x60,
    value: opt.value,
    alt: opt.alt,
    presetKey: opt.key,
  }
  markDirty()
}

function onCustomValueChange(src: SourceDTO, value: number) {
  const n = Number(value)
  if (!Number.isFinite(n) || n <= 0) return
  src.input = { ...src.input, value: n, presetKey: "" }
  markDirty()
}

function addRow() {
  const displayId =
    store.displays.find((d) => !d.builtin)?.id ?? store.displays[0]?.id ?? ""
  draft.value.push({
    // 新行先给个临时 id；保存时后端会用 newID() 重新分配吗？
    // 不会——这里直接生成一个不冲突的本地 id，保证前端 state 一致。
    id: nextId(),
    name: "",
    color: "",
    displayId,
    input: { vcp: 0x60, value: 17, alt: false, presetKey: "hdmi1" },
    shortcut: "",
    confirmMs: settingsDraft.value.defaultConfirmMs,
  })
  markDirty()
}

function nextId(): string {
  const used = new Set(draft.value.map((s) => s.id))
  let n = 1
  while (used.has(`s${n}`)) n++
  return `s${n}`
}

function removeRow(id: string) {
  draft.value = draft.value.filter((s) => s.id !== id)
  // 删掉的源如果是「本机源」，顺手把这个设置也清掉，否则保存会报
  // "self source ... does not exist"。
  if (settingsDraft.value.selfSourceId === id) {
    settingsDraft.value = { ...settingsDraft.value, selfSourceId: "" }
  }
  markDirty()
}

async function save() {
  saving.value = true
  formError.value = ""
  const res = await store.save({ sources: draft.value, settings: settingsDraft.value })
  saving.value = false
  if (!res.ok) {
    formError.value = store.error ? store.errorText(t) : t("errors.internal")
    return
  }
  reset()
}

// ---- 快捷键录制 ----

function startRecord(src: SourceDTO) {
  recording.value = src.id
  recordHint.value = t("sources.recordPrompt")
}

function stopRecord() {
  recording.value = null
  recordHint.value = ""
}

// 监听挂在 window 上而不是遮罩 div 上：div 需要先拿到焦点才收得到键盘，
// 而 window 捕获阶段能拿到所有按键，也不会被页面其它元素抢走。
watch(recording, (id) => {
  if (id) window.addEventListener("keydown", onRecordKey, true)
  else window.removeEventListener("keydown", onRecordKey, true)
})

onUnmounted(() => window.removeEventListener("keydown", onRecordKey, true))

/**
 * 录制时用一个全屏透明层接键盘事件，而不是给某个 input 加监听：
 * 这样 Tab/空格/方向键不会被浏览器默认行为吃掉。
 */
function onRecordKey(e: KeyboardEvent) {
  if (!recording.value) return
  e.preventDefault()
  e.stopPropagation()

  if (e.key === "Escape") {
    stopRecord()
    return
  }
  if (isModifierOnly(e)) {
    recordHint.value = t("sources.recordModifierOnly")
    return
  }
  const accel = eventToAccelerator(e)
  if (!accel) {
    recordHint.value = t("sources.recordNeedModifier")
    return
  }
  const target = draft.value.find((s) => s.id === recording.value)
  if (target) {
    target.shortcut = accel
    markDirty()
  }
  stopRecord()
}

function clearShortcut(src: SourceDTO) {
  src.shortcut = ""
  markDirty()
}

function displayName(id: string): string {
  return store.displayById.get(id)?.name ?? t("common.unknown")
}
</script>

<template>
  <div class="mx-auto flex w-full max-w-3xl flex-col gap-5 px-6 pb-8">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-lg font-semibold tracking-tight">{{ t("sources.title") }}</h1>
        <p class="mt-1 text-sm text-muted-foreground">{{ t("sources.hint") }}</p>
      </div>
      <div class="no-drag flex gap-2">
        <Button v-if="dirty" variant="ghost" size="sm" @click="reset">
          <Undo2 class="size-3.5" />
          {{ t("sources.revert") }}
        </Button>
        <Button variant="outline" size="sm" @click="addRow">
          <Plus class="size-3.5" />
          {{ t("common.add") }}
        </Button>
        <Button size="sm" :disabled="!dirty || saving" @click="save">
          {{ t("common.save") }}
        </Button>
      </div>
    </div>

    <p v-if="!store.displays.length" class="text-sm text-destructive">
      {{ t("sources.noDisplays") }}
    </p>

    <Card v-for="src in draft" :key="src.id" class="p-4">
      <div class="grid gap-3">
        <div class="grid grid-cols-2 gap-3">
          <div class="grid gap-1.5">
            <Label>{{ t("sources.name") }}</Label>
            <Input
              v-model="src.name"
              :placeholder="t('sources.namePlaceholder')"
              maxlength="32"
              @update:model-value="markDirty"
            />
          </div>
          <div class="grid gap-1.5">
            <Label>{{ t("sources.display") }}</Label>
            <Select v-model="src.displayId" @update:model-value="markDirty">
              <option v-for="d in store.displays" :key="d.id" :value="d.id">
                {{ d.name }}
              </option>
            </Select>
          </div>
        </div>

        <div class="grid grid-cols-2 gap-3">
          <div class="grid gap-1.5">
            <Label>{{ t("sources.port") }}</Label>
            <Select
              :model-value="selectedKey(src)"
              @update:model-value="(v: string | undefined) => onPortChange(src, v ?? 'custom')"
            >
              <option v-for="p in portOptions" :key="p.key" :value="p.key">{{ p.label }}</option>
            </Select>
          </div>
          <div class="grid gap-1.5">
            <Label>{{ t("sources.customValue") }}</Label>
            <Input
              :model-value="src.input.value"
              type="number"
              min="1"
              max="65535"
              @update:model-value="(v) => onCustomValueChange(src, Number(v))"
            />
          </div>
        </div>

        <div class="grid grid-cols-2 gap-3">
          <div class="grid gap-1.5">
            <Label>{{ t("sources.shortcut") }}</Label>
            <div class="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                class="flex-1 justify-start font-mono"
                @click="startRecord(src)"
              >
                <Keyboard class="size-3.5" />
                {{ src.shortcut ? formatAccelerator(src.shortcut) : t("sources.record") }}
              </Button>
              <Button
                v-if="src.shortcut"
                variant="ghost"
                size="icon"
                :title="t('sources.clearShortcut')"
                @click="clearShortcut(src)"
              >
                <Trash2 class="size-3.5" />
              </Button>
            </div>
          </div>
          <div class="grid gap-1.5">
            <Label>{{ t("sources.confirmMs") }}</Label>
            <Input
              v-model.number="src.confirmMs"
              type="number"
              min="0"
              max="10000"
              step="100"
              @update:model-value="markDirty"
            />
          </div>
        </div>

        <div class="flex items-center justify-between">
          <p class="text-xs text-muted-foreground">{{ displayName(src.displayId) }} · VCP 0x{{ src.input.vcp.toString(16).toUpperCase() }} = {{ src.input.value }}</p>
          <Button variant="ghost" size="sm" class="text-destructive" @click="removeRow(src.id)">
            <Trash2 class="size-3.5" />
            {{ t("common.remove") }}
          </Button>
        </div>
      </div>
    </Card>

    <Card v-if="!draft.length" class="flex flex-col items-center gap-3 px-6 py-12 text-center">
      <p class="text-sm text-muted-foreground">{{ t("sources.empty") }}</p>
      <Button size="sm" @click="addRow">{{ t("common.add") }}</Button>
    </Card>

    <p v-if="formError" class="text-sm text-destructive">{{ formError }}</p>
    <p v-for="w in store.warnings" :key="w" class="text-xs text-muted-foreground">{{ w }}</p>
  </div>

  <!-- 录制遮罩：全屏接键，避免 Tab/空格被浏览器默认行为吃掉 -->
  <div
    v-if="recording"
    class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm"
    @click="stopRecord"
  >
    <Card class="w-80 p-6 text-center">
      <p class="text-sm font-medium">{{ t("sources.recordPrompt") }}</p>
      <p class="mt-2 min-h-4 text-xs text-muted-foreground">{{ recordHint }}</p>
      <p class="mt-3 text-xs text-muted-foreground">{{ t("sources.recordCancelHint") }}</p>
    </Card>
  </div>
</template>
