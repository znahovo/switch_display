<script setup lang="ts">
// 用原生 <select>：选项只有十来个端口，原生控件在 macOS 上的观感
// 与系统一致，也自带键盘可达性，不值得为它引入 radix 那一套。
import { cn } from "@/lib/utils"

const props = defineProps<{ class?: string }>()
const model = defineModel<string>()
</script>

<template>
  <select
    v-model="model"
    :class="
      cn(
        'flex h-9 w-full rounded-md border border-input bg-transparent px-3 text-sm shadow-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
        props.class,
      )
    "
  >
    <slot />
  </select>
</template>
