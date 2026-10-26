<script setup lang="ts">
// 设置项的一行：左标题/说明，右控件。
//
// 抽出来是为了「视觉对齐」：每行都同一套内边距和控件宽度，
// 否则手写十几个 p-4 迟早会漏一个。
defineProps<{ title: string; hint?: string }>()
</script>

<template>
  <div class="flex items-center justify-between gap-4 px-4 py-3">
    <div class="min-w-0">
      <p class="text-[13px] font-medium leading-5">{{ title }}</p>
      <p v-if="hint" class="mt-0.5 text-[11px] leading-4 text-muted-foreground">{{ hint }}</p>
    </div>
    <div class="flex shrink-0 items-center gap-2">
      <slot />
    </div>
  </div>
</template>
