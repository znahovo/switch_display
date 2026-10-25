<script setup lang="ts">
// 一个信号源 = 一张可点的卡片。
//
// 有意做成整张卡片可点（而不只是按钮）：这是应用的主操作，
// 用户从托盘/快捷键之外的路径点进来时应该一次点击就完成。
import { computed } from "vue"
import { useI18n } from "vue-i18n"
import { Check, Loader2, MonitorPlay, Trash2, AlertTriangle } from "lucide-vue-next"
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

/** outcome 决定右侧徽标的样式；只有 confirmed 是「成功」的绿。 */
const outcome = computed(() => props.outcome ?? "")
const outcomeText = computed(() => (outcome.value ? t(`outcome.${outcome.value}`) : ""))
</script>

<template>
  <div
    class="group flex items-center gap-4 rounded-xl border border-border bg-card px-4 py-3 transition-colors"
    :class="disabled && !busy ? 'opacity-50' : 'hover:border-foreground/25'"
    @dblclick="!disabled && emit('switch')"
  >
    <div class="flex size-9 shrink-0 items-center justify-center rounded-lg bg-secondary">
      <MonitorPlay class="size-4" />
    </div>

    <div class="min-w-0 flex-1">
      <div class="flex items-center gap-2">
        <span class="truncate text-sm font-medium">{{ source.name }}</span>
        <span
          v-if="current"
          class="shrink-0 rounded-full bg-emerald-500/12 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700 dark:text-emerald-400"
        >
          {{ t("switcher.current") }}
        </span>
        <span class="shrink-0 rounded bg-secondary px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
          {{ portLabel }}
        </span>
      </div>
      <p class="mt-0.5 truncate text-xs text-muted-foreground">{{ displayName }}</p>
    </div>

    <div v-if="outcomeText" class="hidden shrink-0 items-center gap-1 text-xs sm:flex"
      :class="outcome === 'confirmed' ? 'text-emerald-600 dark:text-emerald-400' : 'text-muted-foreground'">
      <Check v-if="outcome === 'confirmed'" class="size-3.5" />
      <AlertTriangle v-else-if="outcome === 'failed' || outcome === 'mismatch'" class="size-3.5 text-amber-600 dark:text-amber-400" />
      <span class="max-w-[16rem] truncate">{{ outcomeText }}</span>
    </div>

    <Button size="sm" :disabled="disabled" class="no-drag shrink-0" @click="emit('switch')">
      <Loader2 v-if="busy" class="size-3.5 animate-spin" />
      {{ busy ? t("switcher.switching") : t("switcher.switch") }}
    </Button>

    <Button
      variant="ghost"
      size="icon"
      class="no-drag shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100"
      :title="t('common.remove')"
      @click="emit('remove')"
    >
      <Trash2 class="size-3.5" />
    </Button>
  </div>
</template>
