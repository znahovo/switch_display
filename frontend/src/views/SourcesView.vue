<script setup lang="ts">
// 信号源管理页：一列可对齐的行 + 上方编辑器。
//
// 编辑用「草稿数组」而不是逐条保存到后端：端口唯一性、快捷键唯一性都是
// 跨条目的校验（见 domain/source/repository.go 的 Validate），必须整体提交
// 才能给出准确的错误提示。
//
// 布局取舍：编辑器放在列表**上方**（用户点「添加」后立刻出现在标题下面），
// 因为列表会越来越长，把表单放下面意味着每次新增都要先滚到底。
import { computed, onMounted, ref } from "vue"
import { useI18n } from "vue-i18n"
import { Keyboard, Pencil, Plus, Undo2 } from "lucide-vue-next"
import { useAppStore } from "@/stores/app"
import PageShell from "@/components/PageShell.vue"
import SourceEditor from "@/components/SourceEditor.vue"
import Badge from "@/components/ui/Badge.vue"
import Button from "@/components/ui/Button.vue"
import Card from "@/components/ui/Card.vue"
import { formatAccelerator } from "@/lib/accelerator"
import type { SourceDTO } from "@/api"

const { t } = useI18n()
const store = useAppStore()

/** 草稿：进入页面时从 store 拷一份，编辑期间不污染全局状态。 */
const draft = ref<SourceDTO[]>([])
const settingsDraft = ref({ ...store.settings })
const dirty = ref(false)
const saving = ref(false)
const formError = ref("")

/** 正在编辑的草稿 id；null 表示编辑器关闭。 */
const editingId = ref<string | null>(null)
const editingIsNew = ref(false)

onMounted(async () => {
  await Promise.all([store.loadPresets(), store.refresh()])
  reset()
})

function reset() {
  draft.value = store.sources.map((s) => ({ ...s, input: { ...s.input } }))
  settingsDraft.value = { ...store.settings }
  dirty.value = false
  formError.value = ""
  editingId.value = null
}

function markDirty() {
  dirty.value = true
  formError.value = ""
}

function nextId(): string {
  const used = new Set(draft.value.map((s) => s.id))
  let n = 1
  while (used.has(`s${n}`)) n++
  return `s${n}`
}

/** 新增：插入到草稿**末尾**并立刻打开上方编辑器。 */
function addRow() {
  const displayId = store.displays.find((d) => !d.builtin)?.id ?? store.displays[0]?.id ?? ""
  const row: SourceDTO = {
    id: nextId(),
    name: "",
    color: "",
    displayId,
    input: { vcp: 0x60, value: 17, alt: false, presetKey: "hdmi1" },
    shortcut: "",
    confirmMs: settingsDraft.value.defaultConfirmMs,
  }
  draft.value = [...draft.value, row]
  editingId.value = row.id
  editingIsNew.value = true
  markDirty()
}

function editRow(id: string) {
  editingId.value = id
  editingIsNew.value = false
}

/** 取消编辑：新行直接丢掉，老行保留已有改动（用户可能已经改了几处）。 */
function cancelEdit() {
  if (editingIsNew.value && editingId.value) {
    const id = editingId.value
    draft.value = draft.value.filter((s) => s.id !== id)
    if (!draft.value.length) dirty.value = false
  }
  editingId.value = null
  formError.value = ""
}

function removeRow(id: string) {
  draft.value = draft.value.filter((s) => s.id !== id)
  if (editingId.value === id) editingId.value = null
  // 删掉的源如果是「本机源」，顺手把这个设置也清掉，否则保存会报
  // "self source ... does not exist"。
  if (settingsDraft.value.selfSourceId === id) {
    settingsDraft.value = { ...settingsDraft.value, selfSourceId: "" }
  }
  markDirty()
}

const editing = computed(() => draft.value.find((s) => s.id === editingId.value))

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

function displayName(id: string): string {
  return store.displayById.get(id)?.name ?? t("common.unknown")
}

const hasSources = computed(() => draft.value.length > 0)
</script>

<template>
  <PageShell :title="t('sources.title')" :hint="t('sources.hint')">
    <template #actions>
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
    </template>

    <p v-if="!store.displays.length" class="text-sm text-destructive">
      {{ t("sources.noDisplays") }}
    </p>

    <!-- 编辑器在列表上方：点「添加」后视线不需要移动 -->
    <SourceEditor
      v-if="editing"
      :source="editing"
      :displays="store.displays"
      :presets="store.presets"
      :title="editingIsNew ? t('sources.newTitle') : t('sources.editTitle')"
      :error="formError"
      :saving="saving"
      :can-remove="!editingIsNew"
      @save="save"
      @cancel="cancelEdit"
      @remove="removeRow(editing.id)"
      @change="markDirty"
    />

    <!-- 列表：固定列宽 + 同一行高，扫一眼就能对齐比较 -->
    <Card v-if="hasSources" class="overflow-hidden">
      <div
        class="flex items-center gap-3 border-b border-border/70 bg-muted/30 px-4 py-2 text-[11px] font-medium uppercase tracking-wide text-muted-foreground"
      >
        <span class="min-w-0 flex-1">{{ t("sources.name") }}</span>
        <span class="w-20 shrink-0">{{ t("sources.port") }}</span>
        <span class="w-28 shrink-0">{{ t("sources.shortcut") }}</span>
        <span class="w-16 shrink-0 text-right">{{ t("sources.confirmMsShort") }}</span>
        <span class="w-16 shrink-0" />
      </div>

      <div
        v-for="src in draft"
        :key="src.id"
        class="group flex items-center gap-3 border-b border-border/50 px-4 py-2.5 text-[13px] transition-colors last:border-b-0"
        :class="editingId === src.id ? 'bg-accent/60' : 'hover:bg-accent/35'"
        @dblclick="editRow(src.id)"
      >
        <div class="min-w-0 flex-1">
          <p class="truncate font-medium" :class="src.name ? '' : 'text-muted-foreground italic'">
            {{ src.name || t("sources.unnamed") }}
          </p>
          <p class="mt-0.5 truncate text-[11px] text-muted-foreground">
            {{ displayName(src.displayId) }}
          </p>
        </div>

        <div class="w-20 shrink-0">
          <Badge variant="secondary">{{ store.portLabel(src) }}</Badge>
        </div>

        <div class="w-28 shrink-0">
          <span v-if="src.shortcut" class="inline-flex items-center gap-1 font-mono text-[11px] text-muted-foreground">
            <Keyboard class="size-3 shrink-0" />
            <span class="truncate">{{ formatAccelerator(src.shortcut) }}</span>
          </span>
          <span v-else class="text-[11px] text-muted-foreground/60">—</span>
        </div>

        <div class="w-16 shrink-0 text-right font-mono text-[11px] text-muted-foreground">
          {{ src.confirmMs }}ms
        </div>

        <div class="flex w-16 shrink-0 justify-end gap-0.5">
          <Button
            variant="ghost"
            size="icon"
            class="size-7 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
            :title="t('sources.edit')"
            @click="editRow(src.id)"
          >
            <Pencil class="size-3.5" />
          </Button>
        </div>
      </div>
    </Card>

    <Card v-else class="flex flex-col items-center gap-3 px-6 py-12 text-center">
      <p class="text-sm text-muted-foreground">{{ t("sources.empty") }}</p>
      <Button size="sm" @click="addRow">{{ t("common.add") }}</Button>
    </Card>

    <p v-if="formError && !editing" class="text-[13px] text-destructive">{{ formError }}</p>
    <p v-for="w in store.warnings" :key="w" class="text-xs text-muted-foreground">{{ w }}</p>
  </PageShell>
</template>
