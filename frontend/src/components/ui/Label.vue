<script setup lang="ts">
import { cn } from "@/lib/utils"
const props = defineProps<{ class?: string }>()
</script>

<template>
  <label :class="cn('text-sm font-medium leading-none text-foreground', props.class)">
    <slot />
  </label>
</template>
