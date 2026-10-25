<script setup lang="ts">
// 切换台：第一版唯一的页面。
//
// 只做两件事：列出信号源、点一下切换过去。增删改放在下面的「新增」表单里，
// 独立的管理页留到第二步（托盘/快捷键一起做）。
import { computed, onMounted, ref } from "vue"
import { useI18n } from "vue-i18n"
import { Plus, RefreshCw, Monitor } from "lucide-vue-next"
import { useAppStore } from "@/stores/app"
import Button from "@/components/ui/Button.vue"
import Card from "@/components/ui/Card.vue"
import Input from "@/components/ui/Input.vue"
import Label from "@/components/ui/Label.vue"
import Select from "@/components/ui/Select.vue"
import SourceCard from "@/components/SourceCard.vue"
import type { SourceDTO } from "@/api"

const { t } = useI18n()
const store = useAppStore()

const showForm = ref(false)
const saving = ref(false)
const formError = ref("")

const draft = ref({
  name: "",
  displayId: "",
  presetKey: "hdmi1",
  customValue: 0,
  alt: false,
  confirmMs: 1200,
})

/** 用于下拉的端口表：常用预设 + 自定义。 */
const portOptions = computed(() => [
  ...store.presets.map((p) => ({
    key: p.key,
    label: p.key.toUpperCase().replace("USBC", "USB-C"),
    value: p.value,
    alt: p.alt,
  })),
  { key: "custom", label: t("common.custom"), value: -1, alt: false },
])

onMounted(async () => {
  await Promise.all([store.loadPresets(), store.refresh()])
  // 读回当前输入口用于高亮；不支持的显示器读不到也不会报错。
  for (const d of store.displays.filter((x) => !x.builtin)) {
    void store.readCurrent(d.id)
  }
  if (!draft.value.displayId) {
    draft.value.displayId = store.displays.find((d) => !d.builtin)?.id ?? store.displays[0]?.id ?? ""
  }
})

function openForm() {
  formError.value = ""
  draft.value = {
    name: "",
    displayId: store.displays.find((d) => !d.builtin)?.id ?? store.displays[0]?.id ?? "",
    presetKey: "hdmi1",
    customValue: 0,
    alt: false,
    confirmMs: store.settings.defaultConfirmMs,
  }
  showForm.value = true
}

/** 下一个可用 id：配置里 id 只是稳定标识，内容全在字段里，所以顺序编号即可。 */
function nextId(): string {
  let n = 1
  const used = new Set(store.sources.map((s) => s.id))
  while (used.has(`s${n}`)) n++
  return `s${n}`
}

async function addSource() {
  const opt = portOptions.value.find((p) => p.key === draft.value.presetKey)
  const value = opt && opt.value >= 0 ? opt.value : draft.value.customValue
  const alt = opt?.alt ?? draft.value.alt

  const next: SourceDTO = {
    id: nextId(),
    name: draft.value.name.trim(),
    color: "",
    displayId: draft.value.displayId,
    input: { vcp: alt ? 0xf4 : 0x60, value, alt, presetKey: opt?.key ?? "" },
    shortcut: "",
    confirmMs: draft.value.confirmMs,
  }

  saving.value = true
  formError.value = ""
  const res = await store.save({ sources: [...store.sources, next], settings: store.settings })
  saving.value = false

  if (!res.ok) {
    formError.value = store.error ? store.errorText(t) : t("errors.internal")
    return
  }
  showForm.value = false
}

async function removeSource(id: string) {
  const res = await store.save({
    sources: store.sources.filter((s) => s.id !== id),
    settings: store.settings,
  })
  if (!res.ok) formError.value = store.error ? store.errorText(t) : ""
}

function displayName(id: string): string {
  return store.displayById.get(id)?.name ?? t("common.unknown")
}
</script>

<template>
  <div class="mx-auto flex w-full max-w-3xl flex-col gap-5 px-6 pb-8">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-lg font-semibold tracking-tight">{{ t("nav.switcher") }}</h1>
        <p class="mt-1 text-sm text-muted-foreground">{{ t("app.subtitle") }}</p>
      </div>
      <div class="no-drag flex gap-2">
        <Button variant="outline" size="sm" @click="store.refreshDisplays()">
          <RefreshCw class="size-3.5" />
          {{ t("common.refresh") }}
        </Button>
        <Button size="sm" @click="openForm">
          <Plus class="size-3.5" />
          {{ t("common.add") }}
        </Button>
      </div>
    </div>

    <!-- 显示器上下文：切之前用户要确认自己动的是哪一台 -->
    <div v-if="store.displays.length" class="flex flex-wrap gap-2">
      <div
        v-for="d in store.displays"
        :key="d.id"
        class="flex items-center gap-2 rounded-md border border-border px-2.5 py-1 text-xs text-muted-foreground"
      >
        <Monitor class="size-3.5" />
        <span class="text-foreground">{{ d.name }}</span>
        <span v-if="d.builtin" class="rounded bg-secondary px-1">{{ t("common.unknown") }}</span>
      </div>
    </div>
    <p v-else class="text-sm text-muted-foreground">{{ t("sources.noDisplays") }}</p>

    <!-- 快照列表 -->
    <div v-if="store.sources.length" class="flex flex-col gap-2.5">
      <SourceCard
        v-for="src in store.sources"
        :key="src.id"
        :source="src"
        :display-name="displayName(src.displayId)"
        :port-label="store.portLabel(src)"
        :current="store.isCurrent(src)"
        :busy="store.switchingId === src.id"
        :disabled="store.switchingId !== null"
        :outcome="store.lastOutcome[src.id]"
        @switch="store.switchTo(src)"
        @remove="removeSource(src.id)"
      />
    </div>
    <Card v-else class="flex flex-col items-center gap-3 px-6 py-12 text-center">
      <p class="text-sm text-muted-foreground">{{ t("switcher.empty") }}</p>
      <Button size="sm" @click="openForm">{{ t("switcher.addFirst") }}</Button>
    </Card>

    <!-- 新增表单：第一步先做成内联面板，省掉一个独立路由 -->
    <Card v-if="showForm" class="p-5">
      <div class="grid gap-4">
        <div class="grid gap-1.5">
          <Label>{{ t("sources.name") }}</Label>
          <Input v-model="draft.name" :placeholder="t('sources.namePlaceholder')" maxlength="32" />
        </div>

        <div class="grid gap-1.5">
          <Label>{{ t("sources.display") }}</Label>
          <Select v-model="draft.displayId">
            <option v-for="d in store.displays" :key="d.id" :value="d.id">{{ d.name }}</option>
          </Select>
        </div>

        <div class="grid gap-1.5">
          <Label>{{ t("sources.port") }}</Label>
          <Select v-model="draft.presetKey">
            <option v-for="p in portOptions" :key="p.key" :value="p.key">{{ p.label }}</option>
          </Select>
        </div>

        <div v-if="draft.presetKey === 'custom'" class="grid gap-1.5">
          <Label>{{ t("sources.customValue") }}</Label>
          <Input v-model.number="draft.customValue" type="number" min="1" max="65535" />
        </div>

        <div class="grid gap-1.5">
          <Label>{{ t("sources.confirmMs") }}</Label>
          <Input v-model.number="draft.confirmMs" type="number" min="0" max="10000" step="100" />
          <p class="text-xs text-muted-foreground">{{ t("sources.confirmMsHint") }}</p>
        </div>

        <p v-if="formError" class="text-sm text-destructive">{{ formError }}</p>

        <div class="flex justify-end gap-2">
          <Button variant="ghost" size="sm" @click="showForm = false">{{ t("common.cancel") }}</Button>
          <Button size="sm" :disabled="saving || !draft.name.trim()" @click="addSource">
            {{ t("common.save") }}
          </Button>
        </div>
      </div>
    </Card>

    <p v-if="store.error" class="text-sm text-destructive">{{ store.errorText(t) }}</p>
    <p v-for="w in store.warnings" :key="w" class="text-xs text-muted-foreground">{{ w }}</p>
  </div>
</template>
