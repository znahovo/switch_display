<script setup lang="ts">
// 页面外壳：统一标题区、内容宽度和上下留白。
//
// 三个页面之前各写一遍 `mx-auto max-w-3xl px-6`，改一处忘一处就会错位。
// 收进来之后「视觉对齐」是结构保证的，不靠自觉。
defineProps<{ title: string; hint?: string }>()
</script>

<template>
  <div class="mx-auto flex w-full max-w-3xl flex-col gap-5 px-7 pt-7 pb-10">
    <header class="flex items-start justify-between gap-4">
      <div class="min-w-0">
        <h1 class="text-[17px] font-semibold leading-tight tracking-tight">{{ title }}</h1>
        <p v-if="hint" class="mt-1 text-[13px] leading-relaxed text-muted-foreground">
          {{ hint }}
        </p>
      </div>
      <div class="flex shrink-0 items-center gap-2">
        <slot name="actions" />
      </div>
    </header>
    <slot />
  </div>
</template>
