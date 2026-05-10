<script setup lang="ts">
import { computed } from 'vue'
import type { NodeProps } from '@vue-flow/core'
import type { FlowGapNodeData } from '../composables/use-flow-graph'

const props = defineProps<NodeProps<FlowGapNodeData>>()

const widthStyle = computed(() => ({ width: `${props.data.width}px` }))
</script>

<template>
  <div
    class="memoh-flow-gap-node flex h-[22px] items-center justify-center rounded-full border border-dashed border-border/80 bg-background/90 px-2 text-[10px] font-medium tabular-nums text-muted-foreground shadow-sm backdrop-blur"
    :class="[data.isGrouped ? 'border-primary/35 bg-primary/5 text-primary' : '']"
    :style="widthStyle"
    :title="data.groupLabel ? `${data.label} · ${data.groupLabel}` : data.label"
  >
    <span class="truncate">{{ data.label }}</span>
  </div>
</template>
