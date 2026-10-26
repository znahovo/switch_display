<script setup lang="ts">
// 表单字段：标签 + 控件 + 可选提示。
//
// 存在的意义是「视觉对齐」——信号源页有四个字段，如果每处各写一遍
// label/间距，字号和行高必然慢慢跑偏。统一的组件让所有表单天然对齐。
import { cn } from "@/lib/utils"

const props = defineProps<{ label?: string; hint?: string; class?: string }>()
</script>

<template>
  <div :class="cn('flex min-w-0 flex-col gap-1.5', props.class)">
    <span v-if="props.label" class="text-xs font-medium text-muted-foreground">
      {{ props.label }}
    </span>
    <slot />
    <span v-if="props.hint" class="text-[11px] leading-relaxed text-muted-foreground/80">
      {{ props.hint }}
    </span>
  </div>
</template>
