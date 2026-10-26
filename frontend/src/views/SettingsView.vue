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
import PageShell from "@/components/PageShell.vue"
import SettingRow from "@/components/SettingRow.vue"
import Button from "@/components/ui/Button.vue"
import Card from "@/components/ui/Card.vue"
import Checkbox from "@/components/ui/Checkbox.vue"
import Input from "@/components/ui/Input.vue"
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

function updateBool(key: "confirmAfterSwitch" | "hideToTrayOnClose", v: boolean | undefined) {
  void persist({ [key]: Boolean(v) } as Partial<typeof store.settings>)
}

function updateConfirmMs(v: unknown) {
  const n = Number(v)
  if (!Number.isFinite(n)) return
  void persist({ defaultConfirmMs: n })
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
</script>

<template>
  <PageShell :title="t('settings.title')" :hint="t('settings.hint')">
    <Card class="divide-y divide-border/70">
      <SettingRow :title="t('settings.language')" :hint="t('settings.languageHint')">
        <Select v-model="language" class="w-40">
          <option v-for="l in SUPPORTED" :key="l" :value="l">
            {{ l === "zh-CN" ? "简体中文" : "English" }}
          </option>
        </Select>
      </SettingRow>

      <SettingRow :title="t('settings.hideToTray')" :hint="t('settings.hideToTrayHint')">
        <Checkbox
          :model-value="store.settings.hideToTrayOnClose"
          @update:model-value="updateBool('hideToTrayOnClose', $event)"
        />
      </SettingRow>

      <SettingRow :title="t('settings.confirmAfterSwitch')" :hint="t('settings.confirmAfterSwitchHint')">
        <Checkbox
          :model-value="store.settings.confirmAfterSwitch"
          @update:model-value="updateBool('confirmAfterSwitch', $event)"
        />
      </SettingRow>

      <SettingRow :title="t('settings.defaultConfirmMs')" :hint="t('settings.defaultConfirmMsHint')">
        <Input
          class="w-40"
          type="number"
          min="0"
          max="10000"
          step="100"
          :model-value="store.settings.defaultConfirmMs"
          @change="updateConfirmMs(($event.target as HTMLInputElement).value)"
        />
      </SettingRow>

      <SettingRow :title="t('settings.selfSource')" :hint="t('settings.selfSourceHint')">
        <Select
          class="w-52"
          :model-value="store.settings.selfSourceId ?? ''"
          @update:model-value="(v: string | undefined) => void persist({ selfSourceId: v ?? '' })"
        >
          <option value="">{{ t("common.none") }}</option>
          <option v-for="s in store.sources" :key="s.id" :value="s.id">{{ s.name }}</option>
        </Select>
      </SettingRow>
    </Card>

    <Card class="divide-y divide-border/70">
      <div class="px-4 py-3">
        <p class="text-[13px] font-medium leading-5">{{ t("config.path") }}</p>
        <p class="mt-1 truncate rounded-md bg-muted/60 px-2 py-1.5 font-mono text-[11px] text-muted-foreground">
          {{ store.configPath || "—" }}
        </p>
      </div>
      <SettingRow :title="t('settings.backup')" :hint="t('settings.backupHint')">
        <Button variant="outline" size="sm" @click="exportConfig">
          <Download class="size-3.5" />
          {{ t("settings.export") }}
        </Button>
        <Button variant="outline" size="sm" @click="importConfig">
          <Upload class="size-3.5" />
          {{ t("settings.import") }}
        </Button>
      </SettingRow>
    </Card>

    <p
      v-if="message"
      class="text-[13px]"
      :class="failed ? 'text-destructive' : 'text-muted-foreground'"
    >
      {{ message }}
    </p>
    <p v-for="w in store.warnings" :key="w" class="text-xs text-muted-foreground">{{ w }}</p>
  </PageShell>
</template>
