<template>
  <!-- One reserved row under every turn. The height is always present so the
       layout never jumps; only visibility toggles. While the turn is still
       streaming the row stays fully hidden (no hover reveal) — actions on an
       in-flight answer are meaningless. Finished turns reveal it on
       pointer/focus within the turn's hover scope (group/msg, set on the
       message content wrapper).

       Alignment differs by role on purpose:
       - assistant (`start`): the hover hit-area overflows the text's left edge
         a little (`-ml-1.5`), but NOT by the full button padding — the glyph then
         sits a few px RIGHT of the text's left edge, which reads as visually
         settled rather than a glyph hard-pinned to the margin.
       - user (`end`): the hover hit-area's RIGHT edge meets the bubble's right
         edge (`justify-end`, no negative margin), so the cluster lines up with
         the bubble it belongs to. -->
  <div
    class="chat-message-meta flex h-8 items-center gap-0.5"
    :class="[
      align === 'end' ? 'justify-end' : 'justify-start -ml-1.5',
      streaming ? 'pointer-events-none' : '',
    ]"
  >
    <!-- The tooltip is owned entirely by its trigger (the icon button): moving
         the pointer onto the tooltip itself must NOT keep it alive. Without
         this, the hover row could fade out while a stranded tooltip lingered
         over an already-gone button. -->
    <TooltipProvider
      :delay-duration="0"
      :disable-hoverable-content="true"
    >
      <!-- Copy — shared by both roles. The clipboard glyph is mirrored on X so
           the two stacked squares read top-left over bottom-right, matching the
           composer's copy affordance. -->
      <Tooltip>
        <TooltipTrigger as-child>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            :class="[actionIconClass, hoverActionClass]"
            :aria-label="copyLabel"
            @click="handleCopy"
          >
            <CheckDrawIcon
              v-if="copied"
              class="size-[18px]"
              :stroke-width="1.75"
            />
            <CopyConnectedIcon
              v-else
              class="size-[18px] -scale-x-100"
              :stroke-width="1.75"
            />
          </Button>
        </TooltipTrigger>
        <TooltipContent side="bottom">
          {{ copyLabel }}
        </TooltipContent>
      </Tooltip>

      <!-- ASSISTANT role: reply-level actions. Share/read-aloud stay withheld
           until their flows are wired. -->
      <template v-if="role === 'user'">
        <Tooltip v-if="canEdit">
          <TooltipTrigger as-child>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              :class="[actionIconClass, hoverActionClass]"
              :aria-label="t('chat.actions.edit')"
              @click="emit('edit')"
            >
              <PencilLine />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            {{ t('chat.actions.edit') }}
          </TooltipContent>
        </Tooltip>
      </template>

      <template v-if="role === 'assistant'">
        <Tooltip v-if="canRetry">
          <TooltipTrigger as-child>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              :class="[actionIconClass, hoverActionClass]"
              :aria-label="t('chat.actions.retry')"
              @click="emit('retry')"
            >
              <RotateCcw />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            {{ t('chat.actions.retry') }}
          </TooltipContent>
        </Tooltip>

        <Tooltip v-if="canFork">
          <TooltipTrigger as-child>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              :class="[actionIconClass, hoverActionClass]"
              :aria-label="t('chat.actions.fork')"
              @click="emit('fork')"
            >
              <Split class="rotate-90" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            {{ t('chat.actions.fork') }}
          </TooltipContent>
        </Tooltip>

        <DropdownMenu>
          <DropdownMenuTrigger as-child>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              :class="[actionIconClass, hoverActionClass]"
              :aria-label="t('chat.actions.more')"
            >
              <DotsIcon class="size-[18px]" />
            </Button>
          </DropdownMenuTrigger>
          <!-- Opens UPWARD: the action bar sits right above the composer, so a
               downward menu would land on top of the input. -->
          <DropdownMenuContent
            side="top"
            align="start"
            class="min-w-[12rem]"
          >
            <DropdownMenuLabel
              class="text-label font-normal text-muted-foreground"
              :title="fullTime"
            >
              {{ menuTime }}
            </DropdownMenuLabel>
          </DropdownMenuContent>
        </DropdownMenu>
      </template>

      <div
        v-if="variantState"
        class="flex items-center justify-center text-muted-foreground"
        :aria-label="t('chat.actions.variants', { current: variantState.index + 1, total: variantState.total })"
      >
        <Tooltip>
          <TooltipTrigger as-child>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              :class="actionIconClass"
              :disabled="!variantState.previousHeadTurnId"
              :aria-label="t('chat.actions.previousVariant')"
              @click="switchVariant(variantState.previousHeadTurnId)"
            >
              <ChevronLeft />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            {{ t('chat.actions.previousVariant') }}
          </TooltipContent>
        </Tooltip>
        <span class="px-0.5 text-label font-medium tabular-nums text-muted-foreground">
          {{ variantState.index + 1 }}/{{ variantState.total }}
        </span>
        <Tooltip>
          <TooltipTrigger as-child>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              :class="actionIconClass"
              :disabled="!variantState.nextHeadTurnId"
              :aria-label="t('chat.actions.nextVariant')"
              @click="switchVariant(variantState.nextHeadTurnId)"
            >
              <ChevronRight />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            {{ t('chat.actions.nextVariant') }}
          </TooltipContent>
        </Tooltip>
      </div>
    </TooltipProvider>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronLeft, ChevronRight, PencilLine, RotateCcw, Split } from 'lucide-vue-next'
import CopyConnectedIcon from './copy-connected-icon.vue'
import CheckDrawIcon from './check-draw-icon.vue'
import DotsIcon from './dots-icon.vue'
import type { TurnVariantState } from '@/store/chat-list'
import {
  Button,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuLabel,
} from '@memohai/ui'
import { useClipboard } from '@/composables/useClipboard'

const props = defineProps<{
  copyText: string
  role: 'user' | 'assistant'
  menuTime?: string
  fullTime?: string
  streaming?: boolean
  align?: 'start' | 'end'
  canEdit?: boolean
  canFork?: boolean
  canRetry?: boolean
  variantState?: TurnVariantState | null
}>()

const emit = defineEmits<{
  edit: []
  fork: []
  retry: []
  selectVariant: [headTurnId: string]
}>()

const { t } = useI18n()
const { copyText: writeClipboard } = useClipboard()

const actionIconClass = 'text-muted-foreground hover:text-foreground'
const hoverActionClass = 'opacity-0 pointer-events-none transition-opacity duration-150 motion-reduce:transition-none group-hover/msg:opacity-100 group-hover/msg:pointer-events-auto focus-visible:opacity-100 focus-visible:pointer-events-auto data-[state=open]:opacity-100 data-[state=open]:pointer-events-auto'

const copied = ref(false)
let resetTimer: ReturnType<typeof setTimeout> | null = null

const copyLabel = computed(() => (copied.value ? t('chat.actions.copied') : t('chat.actions.copy')))

async function handleCopy() {
  const ok = await writeClipboard(props.copyText)
  if (!ok) return
  copied.value = true
  if (resetTimer) clearTimeout(resetTimer)
  resetTimer = setTimeout(() => { copied.value = false }, 1500)
}

function switchVariant(headTurnId?: string) {
  const head = headTurnId?.trim()
  if (!head) return
  emit('selectVariant', head)
}
</script>
