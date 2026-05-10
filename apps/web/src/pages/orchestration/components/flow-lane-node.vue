<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { NodeProps } from '@vue-flow/core'
import { ScanSearch, ShieldCheck, Sparkles, Workflow, Wrench, type LucideIcon } from 'lucide-vue-next'
import type { FlowLaneNodeData } from '../composables/use-flow-graph'

const props = defineProps<NodeProps<FlowLaneNodeData>>()

const { t } = useI18n()

const laneMeta = computed<{ label: string, icon: LucideIcon, color: string }>(() => {
  switch (props.data.kind) {
    case 'planning':
      return {
        label: t('orchestration.flowPlanning'),
        icon: Sparkles,
        color: 'border-violet-500/20 bg-violet-500/10 text-violet-700 dark:text-violet-300',
      }
    case 'attempt':
      return {
        label: props.data.label || t('orchestration.flowAttempt'),
        icon: Wrench,
        color: 'border-sky-500/20 bg-sky-500/10 text-sky-700 dark:text-sky-300',
      }
    case 'verification':
      return {
        label: t('orchestration.flowVerification'),
        icon: ShieldCheck,
        color: 'border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
      }
    case 'checkpoint':
      return {
        label: t('orchestration.flowCheckpoint'),
        icon: ScanSearch,
        color: 'border-orange-500/20 bg-orange-500/10 text-orange-700 dark:text-orange-300',
      }
    default:
      return {
        label: props.data.label || t('orchestration.flowStep'),
        icon: Workflow,
        color: 'border-border bg-muted/70 text-muted-foreground',
      }
  }
})
</script>

<template>
  <div class="memoh-flow-lane-node pointer-events-none relative size-full overflow-hidden rounded-xl border border-border/70 bg-background/85 shadow-[0_1px_2px_hsl(var(--foreground)/0.04),0_4px_12px_-2px_hsl(var(--foreground)/0.06),0_12px_28px_-8px_hsl(var(--foreground)/0.07)]">
    <div class="absolute inset-y-0 left-0 flex w-[184px] flex-col justify-center border-r border-border/65 bg-muted/25 px-4">
      <div class="flex items-center gap-2">
        <span
          class="flex size-7 shrink-0 items-center justify-center rounded-md border"
          :class="laneMeta.color"
        >
          <component
            :is="laneMeta.icon"
            class="size-3.5"
          />
        </span>
        <div class="min-w-0">
          <p class="truncate text-xs font-semibold text-foreground">
            {{ laneMeta.label }}
          </p>
          <p class="mt-0.5 truncate text-[10px] text-muted-foreground">
            {{ data.subtitle }}
          </p>
        </div>
      </div>
      <p class="mt-2 text-[10px] uppercase tracking-wide text-muted-foreground/80">
        {{ data.count }} {{ t('orchestration.flowSteps') }}
      </p>
    </div>

    <div class="absolute inset-y-0 left-[184px] right-0">
      <div class="absolute inset-0 bg-[linear-gradient(to_right,hsl(var(--border)/0.42)_1px,transparent_1px)] bg-[length:120px_100%] opacity-70" />
      <div class="absolute inset-x-0 top-1/2 border-t border-dashed border-border/55" />
    </div>
  </div>
</template>
