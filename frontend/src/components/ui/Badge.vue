<script setup lang="ts">
// 小徽标：端口名、当前态、切换结果都用它，保证高度/字号/圆角一致。
import { type VariantProps, cva } from "class-variance-authority"
import { cn } from "@/lib/utils"

const badgeVariants = cva(
  "inline-flex h-5 shrink-0 items-center gap-1 rounded-md px-1.5 text-[11px] font-medium leading-none",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground",
        secondary: "bg-secondary text-secondary-foreground",
        outline: "border border-border text-muted-foreground",
        success: "bg-emerald-500/12 text-emerald-700 dark:text-emerald-400",
        warning: "bg-amber-500/14 text-amber-700 dark:text-amber-400",
        danger: "bg-destructive/12 text-destructive",
      },
    },
    defaultVariants: { variant: "secondary" },
  },
)

type Props = {
  variant?: VariantProps<typeof badgeVariants>["variant"]
  class?: string
}

const props = withDefaults(defineProps<Props>(), { variant: "secondary" })
</script>

<template>
  <span :class="cn(badgeVariants({ variant: props.variant }), props.class)">
    <slot />
  </span>
</template>
