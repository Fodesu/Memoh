<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { NodeProps } from '@vue-flow/core'
import type { LaneNodeData } from '../composables/use-dag-graph'

const props = defineProps<NodeProps<LaneNodeData>>()

const { t } = useI18n()

const stageLabel = computed(() =>
  props.data.isRootLane
    ? t('orchestration.stageRootGoal')
    : t('orchestration.stageTaskCount', { count: props.data.count }),
)
</script>

<template>
  <div
    class="memoh-lane-node pointer-events-none relative size-full overflow-hidden rounded-xl border border-border/55 bg-background shadow-[0_1px_2px_hsl(var(--foreground)/0.04),0_4px_12px_-2px_hsl(var(--foreground)/0.06),0_12px_28px_-8px_hsl(var(--foreground)/0.07)]"
  >
    <div class="flex h-[52px] flex-col items-center justify-center gap-0.5 border-b border-border/55 bg-muted/30">
      <p class="text-[10px] font-semibold leading-none text-foreground/80">
        L{{ data.level }}
      </p>
      <p class="text-[10px] leading-none text-muted-foreground">
        {{ stageLabel }}
      </p>
    </div>
  </div>
</template>
