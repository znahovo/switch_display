<script setup lang="ts">
// 编辑一个信号源（新增和修改共用）。
//
// 为什么是「一个编辑器 + 一个列表」而不是每行内联展开：端口唯一性、快捷键
// 唯一性都是跨条目的约束，必须整体提交；同时一次只编辑一条，用户不容易
// 在多行之间改错。
//
// 快捷键录制监听挂在 window 捕获阶段：挂在遮罩 div 上要先拿焦点，
// 而且 Tab/空格会被浏览器默认行为吃掉。
import { computed, onUnmounted, ref, watch } from "vue"
import { useI18n } from "vue-i18n"
import { Keyboard, Trash2 } from "lucide-vue-next"
import Button from "@/components/ui/Button.vue"
import Card from "@/components/ui/Card.vue"
import Field from "@/components/ui/Field.vue"
import Input from "@/components/ui/Input.vue"
import Select from "@/components/ui/Select.vue"
import { eventToAccelerator, formatAccelerator, isModifierOnly } from "@/lib/accelerator"
import { modifierHintKeys } from "@/lib/platform"
import type { DisplayDTO, PresetDTO, SourceDTO } from "@/api"

const props = defineProps<{
  source: SourceDTO
  displays: DisplayDTO[]
  presets: PresetDTO[]
  title: string
  error?: string
  saving?: boolean
  canRemove?: boolean
}>()

const emit = defineEmits<{
  save: []
  cancel: []
  remove: []
  change: []
}>()

const { t } = useI18n()

/** 直接改 props 里的对象：父组件持有草稿数组，改它即等于改草稿。 */
const src = computed(() => props.source)

const recording = ref(false)
const recordHint = ref("")

const portOptions = computed(() => [
  ...props.presets.map((p) => ({ key: p.key, label: portLabel(p), value: p.value, alt: p.alt })),
  { key: "custom", label: t("common.custom"), value: -1, alt: false },
])

function portLabel(p: PresetDTO): string {
  return p.key.toUpperCase().replace("USBC", "USB-C").replace("HDMI", "HDMI ")
}

/** 从已有 input 反查端口 key；不在预设表里就是自定义。 */
const selectedKey = computed(() => {
  const p = props.presets.find((x) => x.value === src.value.input.value && x.alt === src.value.input.alt)
  return p?.key ?? "custom"
})

function onPortChange(key: string | undefined) {
  const opt = portOptions.value.find((p) => p.key === (key ?? "custom"))
  if (!opt || opt.value < 0) return // 选「自定义」时保留原值，让用户手填
  src.value.input = { vcp: opt.alt ? 0xf4 : 0x60, value: opt.value, alt: opt.alt, presetKey: opt.key }
  emit("change")
}

function onCustomValue(value: unknown) {
  const n = Number(value)
  if (!Number.isFinite(n) || n <= 0) return
  src.value.input = { ...src.value.input, value: n, presetKey: "" }
  emit("change")
}

// ---- 快捷键录制 ----

function startRecord() {
  recording.value = true
  recordHint.value = t("sources.recordPrompt")
}

function stopRecord() {
  recording.value = false
  recordHint.value = ""
}

watch(recording, (on) => {
  if (on) window.addEventListener("keydown", onRecordKey, true)
  else window.removeEventListener("keydown", onRecordKey, true)
})

onUnmounted(() => window.removeEventListener("keydown", onRecordKey, true))

function onRecordKey(e: KeyboardEvent) {
  if (!recording.value) return
  e.preventDefault()
  e.stopPropagation()

  if (e.key === "Escape") return stopRecord()
  if (isModifierOnly(e)) {
    recordHint.value = t("sources.recordModifierOnly")
    return
  }
  const accel = eventToAccelerator(e)
  if (!accel) {
    recordHint.value = t("sources.recordNeedModifier", { keys: modifierHintKeys })
    return
  }
  src.value.shortcut = accel
  emit("change")
  stopRecord()
}
</script>

<template>
  <Card class="overflow-hidden">
    <!-- 编辑器标题栏：明确「现在在改哪一条」，避免和列表混淆 -->
    <div class="flex items-center justify-between gap-3 border-b border-border/70 bg-muted/30 px-4 py-2.5">
      <p class="text-[13px] font-medium">{{ title }}</p>
      <Button v-if="canRemove" variant="ghost" size="sm" class="text-destructive" @click="emit('remove')">
        <Trash2 class="size-3.5" />
        {{ t("common.remove") }}
      </Button>
    </div>

    <div class="grid gap-3.5 p-4">
      <div class="grid grid-cols-2 gap-3.5">
        <Field :label="t('sources.name')">
          <Input
            v-model="src.name"
            :placeholder="t('sources.namePlaceholder')"
            maxlength="32"
            @update:model-value="emit('change')"
          />
        </Field>
        <Field :label="t('sources.display')">
          <Select v-model="src.displayId" @update:model-value="emit('change')">
            <option v-for="d in displays" :key="d.id" :value="d.id">{{ d.name }}</option>
          </Select>
        </Field>
      </div>

      <div class="grid grid-cols-2 gap-3.5">
        <Field :label="t('sources.port')">
          <Select
            :model-value="selectedKey"
            @update:model-value="(v: string | undefined) => onPortChange(v)"
          >
            <option v-for="p in portOptions" :key="p.key" :value="p.key">{{ p.label }}</option>
          </Select>
        </Field>
        <Field
          :label="t('sources.customValue')"
          :hint="selectedKey === 'custom' ? t('sources.portCustom') : undefined"
        >
          <Input
            :model-value="src.input.value"
            type="number"
            min="1"
            max="65535"
            :disabled="selectedKey !== 'custom'"
            @update:model-value="onCustomValue"
          />
        </Field>
      </div>

      <div class="grid grid-cols-2 gap-3.5">
        <Field :label="t('sources.shortcut')">
          <div class="flex gap-2">
            <Button
              variant="outline"
              class="min-w-0 flex-1 justify-start font-mono text-xs"
              @click="startRecord"
            >
              <Keyboard class="size-3.5 shrink-0" />
              <span class="truncate">
                {{ src.shortcut ? formatAccelerator(src.shortcut) : t("sources.record") }}
              </span>
            </Button>
            <Button
              v-if="src.shortcut"
              variant="ghost"
              size="icon"
              :title="t('sources.clearShortcut')"
              @click="
                () => {
                  src.shortcut = ''
                  emit('change')
                }
              "
            >
              <Trash2 class="size-3.5" />
            </Button>
          </div>
        </Field>
        <Field :label="t('sources.confirmMs')" :hint="t('sources.confirmMsHint')">
          <Input
            v-model.number="src.confirmMs"
            type="number"
            min="0"
            max="10000"
            step="100"
            @update:model-value="emit('change')"
          />
        </Field>
      </div>

      <p v-if="error" class="text-[13px] text-destructive">{{ error }}</p>

      <div class="flex items-center justify-between gap-2 pt-0.5">
        <p class="font-mono text-[11px] text-muted-foreground">
          VCP 0x{{ src.input.vcp.toString(16).toUpperCase().padStart(2, "0") }} =
          {{ src.input.value }}
        </p>
        <div class="flex gap-2">
          <Button variant="ghost" size="sm" @click="emit('cancel')">{{ t("common.cancel") }}</Button>
          <Button size="sm" :disabled="saving || !src.name.trim()" @click="emit('save')">
            {{ t("common.save") }}
          </Button>
        </div>
      </div>
    </div>
  </Card>

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
