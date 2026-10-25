<script setup lang="ts">
// 设置页：全局偏好 + 配置文件导入导出。
//
// 每一项都直接写回后端（不攒草稿）：这些都是独立的开关，没有跨项校验，
// 攒草稿反而让用户不确定「到底生效了没」。
import { computed, onMounted, ref } from "vue"
import { useI18n } from "vue-i18n"
import { Download, Upload } from "lucide-vue-next"
import { Dialogs } from "@wailsio/runtime"
import { useAppStore } from "@/stores/app"
import Button from "@/components/ui/Button.vue"
import Card from "@/components/ui/Card.vue"
import Input from "@/components/ui/Input.vue"
import Label from "@/components/ui/Label.vue"
import Select from "@/components/ui/Select.vue"
import { SUPPORTED, setLocale, type Locale } from "@/locales"

const { t, locale } = useI18n()
const store = useAppStore()

const saving = ref(false)
const message = ref("")
const failed = ref(false)

onMounted(async () => {
  if (!store.sources.length && !store.configPath) await store.refresh()
})

/** 语言下拉：跟 i18n 的 locale 双向绑定，同时落库。 */
const language = computed<Locale>({
  get: () => locale.value as Locale,
  set: (v) => {
    setLocale(v)
    void persist({ language: v })
  },
})

/** persist 把局部改动的 settings 合并后整体保存。 */
async function persist(patch: Partial<typeof store.settings>) {
  saving.value = true
  message.value = ""
  failed.value = false
  const res = await store.save({
    sources: store.sources,
    settings: { ...store.settings, ...patch },
  })
  saving.value = false
  if (!res.ok) {
    failed.value = true
    message.value = store.error ? store.errorText(t) : t("errors.internal")
    return
  }
  message.value = t("settings.saved")
}

function updateBool(key: "confirmAfterSwitch" | "hideToTrayOnClose", v: boolean) {
  void persist({ [key]: v } as Partial<typeof store.settings>)
}

function updateConfirmMs(v: number | string) {
  const n = Number(v)
  if (!Number.isFinite(n)) return
  void persist({ defaultConfirmMs: n })
}

function updateSelf(v: string) {
  void persist({ selfSourceId: v })
}

/** 导出：让用户选保存位置，再把当前配置写成 JSON。 */
async function exportConfig() {
  message.value = ""
  failed.value = false
  try {
    const dest = await Dialogs.SaveFile({
      Title: t("settings.export"),
      Filename: "switch-display-config.json",
      Filters: [{ DisplayName: "JSON", Pattern: "*.json" }],
    })
    if (!dest) return // 用户取消
    await store.exportTo(dest)
    message.value = t("settings.exported")
  } catch (e) {
    failed.value = true
    message.value = String(e)
  }
}

/** 导入：选中文件后先只解析，用户确认再保存（避免误覆盖）。 */
async function importConfig() {
  message.value = ""
  failed.value = false
  try {
    const picked = await Dialogs.OpenFile({
      Title: t("settings.import"),
      CanChooseFiles: true,
      CanChooseDirectories: false,
      AllowsMultipleSelection: false,
      Filters: [{ DisplayName: "JSON", Pattern: "*.json" }],
    })
    const src = picked?.[0]
    if (!src) return
    const state = await store.importFrom(src)
    message.value = t("settings.imported", { count: state.sources.length })
  } catch (e) {
    failed.value = true
    message.value = String(e)
  }
}

const sourceOptions = computed(() => store.sources)
</script>

<template>
  <div class="mx-auto flex w-full max-w-3xl flex-col gap-5 px-6 pb-8">
    <div>
      <h1 class="text-lg font-semibold tracking-tight">{{ t("settings.title") }}</h1>
      <p class="mt-1 text-sm text-muted-foreground">{{ t("settings.hint") }}</p>
    </div>

    <Card class="divide-y divide-border">
      <!-- 语言 -->
      <div class="flex items-center justify-between gap-4 p-4">
        <div>
          <p class="text-sm font-medium">{{ t("settings.language") }}</p>
          <p class="mt-0.5 text-xs text-muted-foreground">{{ t("settings.languageHint") }}</p>
        </div>
        <Select v-model="language" class="w-40 shrink-0">
          <option v-for="l in SUPPORTED" :key="l" :value="l">
            {{ l === "zh-CN" ? "简体中文" : "English" }}
          </option>
        </Select>
      </div>

      <!-- 关闭窗口时隐藏 -->
      <div class="flex items-center justify-between gap-4 p-4">
        <div>
          <p class="text-sm font-medium">{{ t("settings.hideToTray") }}</p>
          <p class="mt-0.5 text-xs text-muted-foreground">{{ t("settings.hideToTrayHint") }}</p>
        </div>
        <input
          type="checkbox"
          class="size-4 accent-primary"
          :checked="store.settings.hideToTrayOnClose"
          @change="updateBool('hideToTrayOnClose', ($event.target as HTMLInputElement).checked)"
        />
      </div>

      <!-- 切换后读回确认 -->
      <div class="flex items-center justify-between gap-4 p-4">
        <div>
          <p class="text-sm font-medium">{{ t("settings.confirmAfterSwitch") }}</p>
          <p class="mt-0.5 text-xs text-muted-foreground">
            {{ t("settings.confirmAfterSwitchHint") }}
          </p>
        </div>
        <input
          type="checkbox"
          class="size-4 accent-primary"
          :checked="store.settings.confirmAfterSwitch"
          @change="updateBool('confirmAfterSwitch', ($event.target as HTMLInputElement).checked)"
        />
      </div>

      <!-- 默认确认等待 -->
      <div class="flex items-center justify-between gap-4 p-4">
        <div>
          <p class="text-sm font-medium">{{ t("settings.defaultConfirmMs") }}</p>
          <p class="mt-0.5 text-xs text-muted-foreground">
            {{ t("settings.defaultConfirmMsHint") }}
          </p>
        </div>
        <Input
          class="w-40 shrink-0"
          type="number"
          min="0"
          max="10000"
          step="100"
          :model-value="store.settings.defaultConfirmMs"
          @change="updateConfirmMs(($event.target as HTMLInputElement).value)"
        />
      </div>

      <!-- 本机源 -->
      <div class="flex items-center justify-between gap-4 p-4">
        <div>
          <p class="text-sm font-medium">{{ t("settings.selfSource") }}</p>
          <p class="mt-0.5 text-xs text-muted-foreground">{{ t("settings.selfSourceHint") }}</p>
        </div>
        <Select
          class="w-48 shrink-0"
          :model-value="store.settings.selfSourceId ?? ''"
          @update:model-value="(v: string | undefined) => updateSelf(v ?? '')"
        >
          <option value="">{{ t("common.none") }}</option>
          <option v-for="s in sourceOptions" :key="s.id" :value="s.id">{{ s.name }}</option>
        </Select>
      </div>
    </Card>

    <!-- 配置文件 -->
    <Card class="divide-y divide-border">
      <div class="flex items-center justify-between gap-4 p-4">
        <div class="min-w-0">
          <p class="text-sm font-medium">{{ t("config.path") }}</p>
          <p class="mt-0.5 truncate font-mono text-xs text-muted-foreground">
            {{ store.configPath || "—" }}
          </p>
        </div>
      </div>
      <div class="flex items-center justify-between gap-4 p-4">
        <div>
          <p class="text-sm font-medium">{{ t("settings.backup") }}</p>
          <p class="mt-0.5 text-xs text-muted-foreground">{{ t("settings.backupHint") }}</p>
        </div>
        <div class="flex shrink-0 gap-2">
          <Button variant="outline" size="sm" @click="exportConfig">
            <Download class="size-3.5" />
            {{ t("settings.export") }}
          </Button>
          <Button variant="outline" size="sm" @click="importConfig">
            <Upload class="size-3.5" />
            {{ t("settings.import") }}
          </Button>
        </div>
      </div>
    </Card>

    <p v-if="message" class="text-sm" :class="failed ? 'text-destructive' : 'text-muted-foreground'">
      {{ message }}
    </p>
    <p v-for="w in store.warnings" :key="w" class="text-xs text-muted-foreground">{{ w }}</p>
  </div>
</template>
