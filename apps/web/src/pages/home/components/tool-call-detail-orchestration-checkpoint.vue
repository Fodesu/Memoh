<template>
  <div class="space-y-2">
    <p
      v-if="prompt"
      class="text-xs text-muted-foreground italic"
    >
      {{ prompt }}
    </p>
    <div class="rounded-md border border-border bg-card text-card-foreground px-3 py-2.5 space-y-2.5">
      <div class="flex items-start gap-2">
        <MessageCircleQuestion class="size-4 mt-0.5 shrink-0 text-primary" />
        <div class="space-y-0.5 flex-1 min-w-0">
          <p class="text-sm font-medium leading-snug whitespace-pre-wrap break-words">
            {{ question }}
          </p>
          <p
            v-if="checkpointBadge"
            class="text-xs text-muted-foreground"
          >
            {{ checkpointBadge }}
          </p>
        </div>
      </div>

      <div
        v-if="resolved"
        class="flex items-center gap-1.5 text-xs text-success-foreground"
      >
        <CircleCheck class="size-3.5 shrink-0" />
        <span>{{ resolvedSummary }}</span>
      </div>
      <div
        v-else-if="stale"
        class="flex items-center gap-1.5 text-xs text-muted-foreground"
      >
        <CircleSlash class="size-3.5 shrink-0" />
        <span>{{ t('chat.tools.detail.checkpoint.stale') }}</span>
      </div>
      <template v-else>
        <div
          v-if="choiceOptions.length"
          class="grid gap-1.5"
        >
          <Button
            v-for="option in choiceOptions"
            :key="option.id"
            variant="outline"
            size="sm"
            class="justify-start text-left h-auto py-2 px-2.5 whitespace-normal"
            :disabled="busy"
            @click="selectOption(option)"
          >
            <span class="flex flex-col gap-0.5 w-full">
              <span class="flex items-center gap-1.5">
                <Loader2
                  v-if="submittingOption === option.id"
                  class="size-3.5 shrink-0 animate-spin"
                />
                <span class="font-medium">{{ option.label || option.id }}</span>
              </span>
              <span
                v-if="option.description"
                class="text-xs text-muted-foreground"
              >{{ option.description }}</span>
            </span>
          </Button>
        </div>

        <div
          v-if="hasFreeform"
          class="space-y-1.5"
        >
          <Textarea
            v-model="freeformInput"
            :placeholder="t('chat.tools.detail.checkpoint.freeformPlaceholder')"
            :disabled="busy"
            class="min-h-[60px] text-sm"
          />
          <div class="flex justify-end">
            <Button
              size="sm"
              :disabled="busy || !freeformInput.trim()"
              @click="submitFreeform"
            >
              <Loader2
                v-if="submittingOption === freeformOption?.id"
                class="size-3.5 mr-1.5 animate-spin"
              />
              {{ t('chat.tools.detail.checkpoint.submit') }}
            </Button>
          </div>
        </div>

        <div
          v-if="hasDefault"
          class="flex justify-end"
        >
          <Button
            variant="ghost"
            size="sm"
            :disabled="busy"
            @click="submitDefault"
          >
            <Loader2
              v-if="submittingOption === defaultAction?.option"
              class="size-3.5 mr-1.5 animate-spin"
            />
            {{ defaultLabel }}
          </Button>
        </div>

        <p
          v-if="errorMessage"
          class="text-xs text-destructive"
        >
          {{ errorMessage }}
        </p>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { CircleCheck, CircleSlash, Loader2, MessageCircleQuestion } from 'lucide-vue-next'
import { Button, Textarea } from '@memohai/ui'
import { useI18n } from 'vue-i18n'
import {
  postOrchestrationCheckpointsByCheckpointIdResolve,
  type OrchestrationCheckpointDefaultAction,
  type OrchestrationCheckpointOption,
  type OrchestrationHumanCheckpoint,
} from '@memohai/sdk'
import type { ToolCallBlock } from '@/store/chat-list'
import { resolveApiErrorMessage } from '@/utils/api-error'

interface CheckpointPayload {
  id?: string
  run_id?: string
  task_id?: string
  status?: string
  question?: string
  blocks_run?: boolean
  options?: OrchestrationCheckpointOption[]
  default_action?: OrchestrationCheckpointDefaultAction
  timeout_at?: string
  resolved_option?: string
  resolved_response?: string
}

interface ToolResultShape {
  prompt?: string
  status_message?: string
  checkpoint?: CheckpointPayload
  structuredContent?: ToolResultShape
}

const FREEFORM_KIND = 'freeform'
const TERMINAL_STATUSES = new Set(['resolved', 'timed_out', 'cancelled', 'superseded'])
const CHECKPOINT_RESOLUTION_CACHE_KEY = 'orchestration-checkpoint-resolutions'
const resolvedCheckpointCache = loadResolvedCheckpointCache()

const props = defineProps<{ block: ToolCallBlock }>()
const { t } = useI18n()

const submittingOption = ref<string | null>(null)
const submitting = ref(false)
const freeformInput = ref('')
const errorMessage = ref('')
const localResolution = ref<{ option?: string, response?: string } | null>(null)
const localStale = ref(false)

const toolResult = computed<ToolResultShape>(() => {
  const raw = (props.block.result ?? {}) as ToolResultShape
  return raw.structuredContent ?? raw
})

const checkpoint = computed<CheckpointPayload>(() => {
  const snapshot = toolResult.value.checkpoint ?? {}
  const id = snapshot.id
  if (!id) return snapshot
  const cached = resolvedCheckpointCache.get(id)
  return cached ? { ...snapshot, ...cached } : snapshot
})

const prompt = computed(() => (toolResult.value.prompt ?? '').trim())
const question = computed(() => checkpoint.value.question ?? '')

const options = computed<OrchestrationCheckpointOption[]>(() => checkpoint.value.options ?? [])
const choiceOptions = computed(() => options.value.filter(opt => opt.kind !== FREEFORM_KIND))
const freeformOption = computed(() => options.value.find(opt => opt.kind === FREEFORM_KIND))
const hasFreeform = computed(() => Boolean(freeformOption.value))
const defaultAction = computed<OrchestrationCheckpointDefaultAction | undefined>(() => checkpoint.value.default_action)
const hasDefault = computed(() => Boolean(defaultAction.value?.option))

const busy = computed(() => submitting.value)

const remoteResolved = computed(() => TERMINAL_STATUSES.has(checkpoint.value.status ?? ''))
const resolved = computed(() => localResolution.value !== null || remoteResolved.value)
const stale = computed(() => !resolved.value && localStale.value)

const resolvedSummary = computed(() => {
  if (checkpoint.value.resolved_option) {
    return formatResolution(checkpoint.value.resolved_option, checkpoint.value.resolved_response)
  }

  const local = localResolution.value
  if (local) {
    return formatResolution(local.option, local.response)
  }
  return t('chat.tools.detail.checkpoint.resolvedRemote')
})

const checkpointBadge = computed(() => {
  const parts: string[] = []
  if (checkpoint.value.blocks_run) parts.push(t('chat.tools.detail.checkpoint.blocksRun'))
  if (checkpoint.value.timeout_at) {
    const ts = new Date(checkpoint.value.timeout_at)
    if (!Number.isNaN(ts.getTime())) {
      parts.push(t('chat.tools.detail.checkpoint.timeoutAt', { time: ts.toLocaleString() }))
    }
  }
  return parts.join(' · ')
})

const defaultLabel = computed(() => {
  const action = defaultAction.value
  if (!action) return t('chat.tools.detail.checkpoint.useDefault')
  const matched = options.value.find(opt => opt.id === action.option)
  if (matched?.kind === FREEFORM_KIND) {
    return t('chat.tools.detail.checkpoint.useDefaultFreeform')
  }
  return t('chat.tools.detail.checkpoint.useDefaultOption', { label: matched?.label || matched?.id || action.option })
})

function buildIdempotencyKey(option?: string, response?: string): string {
  const checkpointId = checkpoint.value.id ?? props.block.toolCallId
  const seed = response ? `${response.length}:${response.slice(0, 64)}` : ''
  return `chat-checkpoint:${checkpointId}:${option ?? ''}:${seed}`
}

function formatResolution(optionID?: string, response?: string): string {
  const matched = options.value.find(opt => opt.id === optionID)
  if (matched?.kind === FREEFORM_KIND) {
    return t('chat.tools.detail.checkpoint.resolvedFreeform', { input: response ?? '' })
  }
  return t('chat.tools.detail.checkpoint.resolvedOption', { label: matched?.label || matched?.id || optionID || '' })
}

async function submit(option: string | undefined, responseText: string | undefined) {
  const id = checkpoint.value.id
  if (!id || !option || busy.value) return

  submitting.value = true
  submittingOption.value = option
  errorMessage.value = ''

  try {
    const { data, error, response } = await postOrchestrationCheckpointsByCheckpointIdResolve({
      path: { checkpoint_id: id },
      body: {
        option,
        response: responseText,
        idempotency_key: buildIdempotencyKey(option, responseText),
      },
    })
    if (error) {
      if (response?.status === 409 || response?.status === 404) {
        localStale.value = true
        return
      }
      errorMessage.value = resolveApiErrorMessage(error, t('chat.tools.detail.checkpoint.submitFailed'))
      return
    }
    const resolvedCheckpoint = data?.checkpoint
    if (resolvedCheckpoint) {
      cacheResolvedCheckpoint(resolvedCheckpoint)
    }
    localResolution.value = { option, response: responseText }
  }
  catch (caught) {
    errorMessage.value = resolveApiErrorMessage(caught, t('chat.tools.detail.checkpoint.submitFailed'))
  }
  finally {
    submitting.value = false
    submittingOption.value = null
  }
}

function selectOption(option: OrchestrationCheckpointOption) {
  void submit(option.id, undefined)
}

function submitFreeform() {
  const trimmed = freeformInput.value.trim()
  const optionID = freeformOption.value?.id
  if (!trimmed || !optionID) return
  void submit(optionID, trimmed)
}

function submitDefault() {
  const action = defaultAction.value
  if (!action?.option) return
  void submit(action.option, action.response)
}

function cacheResolvedCheckpoint(checkpoint: OrchestrationHumanCheckpoint) {
  if (!checkpoint.id) return
  resolvedCheckpointCache.set(checkpoint.id, checkpoint)
  persistResolvedCheckpointCache()
}

function loadResolvedCheckpointCache(): Map<string, CheckpointPayload> {
  if (typeof window === 'undefined') return new Map()
  try {
    const raw = window.sessionStorage.getItem(CHECKPOINT_RESOLUTION_CACHE_KEY)
    if (!raw) return new Map()
    const parsed = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object') return new Map()
    return new Map(Object.entries(parsed) as Array<[string, CheckpointPayload]>)
  }
  catch {
    return new Map()
  }
}

function persistResolvedCheckpointCache() {
  if (typeof window === 'undefined') return
  try {
    window.sessionStorage.setItem(
      CHECKPOINT_RESOLUTION_CACHE_KEY,
      JSON.stringify(Object.fromEntries(resolvedCheckpointCache)),
    )
  }
  catch {
    // Ignore storage failures; the current component state still shows the resolution.
  }
}
</script>
