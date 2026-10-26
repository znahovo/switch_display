<script setup lang="ts">
// 一个信号源 = 一张可点的卡片。
//
// 有意做成整张卡片可点（而不只是按钮）：这是应用的主操作，
// 用户从托盘/快捷键之外的路径点进来时应该一次点击就完成。
import { computed } from "vue"
import { useI18n } from "vue-i18n"
import { AlertTriangle, Check, Loader2, MonitorPlay, Trash2 } from "lucide-vue-next"
import Badge from "@/components/ui/Badge.vue"
import Button from "@/components/ui/Button.vue"
import type { SourceDTO } from "@/api"

const props = defineProps<{
  source: SourceDTO
  displayName: string
  portLabel: string
  busy: boolean
  disabled: boolean
  current: boolean
  outcome?: string
}>()

const emit = defineEmits<{ switch: []; remove: [] }>()

const { t } = useI18n()

const outcome = computed(() => props.outcome ?? "")
const outcomeText = computed(() => (outcome.value ? t(`outcome.${outcome.value}`) : ""))
/** 只有 confirmed 是「成功」的绿，其余都是需要用户看一眼的状态。 */
const outcomeVariant = computed(() => {
  if (outcome.value === "confirmed") return "success" as const
  if (outcome.value === "failed" || outcome.value === "mismatch") return "warning" as const
  return "outline" as const
})
</script>

<template>
  <div
    class="group flex items-center gap-3.5 rounded-xl border bg-card px-4 py-3 transition-colors"
    :class="[
      current && !outcomeText ? 'border-emerald-500/35' : 'border-border',
      disabled && !busy ? 'opacity-55' : 'hover:border-foreground/25',
    ]"
    @dblclick="!disabled && emit('switch')"
  >
    <div
      class="flex size-9 shrink-0 items-center justify-center rounded-lg transition-colors"
      :class="current ? 'bg-emerald-500/12 text-emerald-700 dark:text-emerald-400' : 'bg-secondary'"
    >
      <MonitorPlay class="size-4" />
    </div>

    <!-- 名称 + 副信息：固定两行，卡片高度因此永远一致 -->
    <div class="min-w-0 flex-1">
      <div class="flex items-center gap-1.5">
        <span class="truncate text-[13px] font-medium leading-5">{{ source.name }}</span>
        <Badge v-if="current" variant="success">{{ t("switcher.current") }}</Badge>
        <Badge variant="secondary" class="font-mono">{{ portLabel }}</Badge>
      </div>
      <p class="truncate text-[11px] leading-4 text-muted-foreground">{{ displayName }}</p>
    </div>

    <Badge v-if="outcomeText" :variant="outcomeVariant" class="hidden max-w-[15rem] sm:inline-flex">
      <Check v-if="outcome === 'confirmed'" class="size-3" />
      <AlertTriangle v-else-if="outcome === 'failed' || outcome === 'mismatch'" class="size-3" />
      <span class="truncate">{{ outcomeText }}</span>
    </Badge>

    <Button size="sm" :disabled="disabled" class="no-drag w-[74px] shrink-0" @click="emit('switch')">
      <Loader2 v-if="busy" class="size-3.5 animate-spin" />
      {{ busy ? t("switcher.switchingShort") : t("switcher.switch") }}
    </Button>

    <Button
      variant="ghost"
      size="icon"
      class="no-drag size-7 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
      :title="t('common.remove')"
      @click="emit('remove')"
    >
      <Trash2 class="size-3.5" />
    </Button>
  </div>
</template>
