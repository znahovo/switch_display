<script setup lang="ts">
// 原生 checkbox 包一层：原生控件的键盘可达性与系统观感都够用，
// 包一层只是为了让它的尺寸/对齐在设置页里统一。
import { cn } from "@/lib/utils"

const props = defineProps<{ class?: string }>()
const model = defineModel<boolean>()
</script>

<template>
  <input
    v-model="model"
    type="checkbox"
    :class="cn('size-4 shrink-0 cursor-pointer accent-primary', props.class)"
  />
</template>
