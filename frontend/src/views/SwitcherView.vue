<script setup lang="ts">
// 切换台：主操作页。
//
// 只做两件事：列出信号源、点一下切换过去。新增用同一个 SourceEditor，
// 放在列表**上方**——表单在下面时，用户点完「添加」还得往下滚才能填。
import { computed, onMounted, ref } from "vue"
import { useI18n } from "vue-i18n"
import { Monitor, Plus, RefreshCw } from "lucide-vue-next"
import { useAppStore } from "@/stores/app"
import PageShell from "@/components/PageShell.vue"
import SourceCard from "@/components/SourceCard.vue"
import SourceEditor from "@/components/SourceEditor.vue"
import Badge from "@/components/ui/Badge.vue"
import Button from "@/components/ui/Button.vue"
import Card from "@/components/ui/Card.vue"
import type { SourceDTO } from "@/api"

const { t } = useI18n()
const store = useAppStore()

const showForm = ref(false)
const saving = ref(false)
const formError = ref("")
const draft = ref<SourceDTO | null>(null)

onMounted(async () => {
  await Promise.all([store.loadPresets(), store.refresh()])
  // 读回当前输入口用于高亮；不支持的显示器读不到也不会报错。
  for (const d of store.displays.filter((x) => !x.builtin)) {
    void store.readCurrent(d.id)
  }
})

function openForm() {
  formError.value = ""
  draft.value = {
    id: nextId(),
    name: "",
    color: "",
    displayId: store.displays.find((d) => !d.builtin)?.id ?? store.displays[0]?.id ?? "",
    input: { vcp: 0x60, value: 17, alt: false, presetKey: "hdmi1" },
    shortcut: "",
    confirmMs: store.settings.defaultConfirmMs,
  }
  showForm.value = true
}

function closeForm() {
  showForm.value = false
  draft.value = null
  formError.value = ""
}

/** 下一个可用 id：配置里 id 只是稳定标识，内容全在字段里，所以顺序编号即可。 */
function nextId(): string {
  let n = 1
  const used = new Set(store.sources.map((s) => s.id))
  while (used.has(`s${n}`)) n++
  return `s${n}`
}

async function addSource() {
  if (!draft.value) return
  saving.value = true
  formError.value = ""
  const res = await store.save({ sources: [...store.sources, draft.value], settings: store.settings })
  saving.value = false
  if (!res.ok) {
    formError.value = store.error ? store.errorText(t) : t("errors.internal")
    return
  }
  closeForm()
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

const externalDisplays = computed(() => store.displays.filter((d) => !d.builtin))
</script>

<template>
  <PageShell :title="t('nav.switcher')" :hint="t('app.subtitle')">
    <template #actions>
      <Button variant="outline" size="sm" @click="store.refreshDisplays()">
        <RefreshCw class="size-3.5" />
        {{ t("common.refresh") }}
      </Button>
      <Button size="sm" :disabled="showForm" @click="openForm">
        <Plus class="size-3.5" />
        {{ t("common.add") }}
      </Button>
    </template>

    <!-- 显示器上下文：切之前用户要确认自己动的是哪一台 -->
    <div v-if="store.displays.length" class="flex flex-wrap gap-1.5">
      <div
        v-for="d in store.displays"
        :key="d.id"
        class="inline-flex h-7 items-center gap-1.5 rounded-lg border border-border/70 bg-card px-2 text-[12px]"
      >
        <Monitor class="size-3.5 text-muted-foreground" />
        <span class="font-medium">{{ d.name }}</span>
        <Badge v-if="d.builtin" variant="outline">{{ t("switcher.builtin") }}</Badge>
      </div>
    </div>

    <!-- 新增表单在列表上方 -->
    <SourceEditor
      v-if="showForm && draft"
      :source="draft"
      :displays="store.displays"
      :presets="store.presets"
      :title="t('sources.newTitle')"
      :error="formError"
      :saving="saving"
      @save="addSource"
      @cancel="closeForm"
      @change="formError = ''"
    />

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

    <p v-if="store.error" class="text-[13px] text-destructive">{{ store.errorText(t) }}</p>
    <p v-for="w in store.warnings" :key="w" class="text-xs text-muted-foreground">{{ w }}</p>
  </PageShell>
</template>
