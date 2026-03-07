<template>
  <div class="space-y-4">
    <!-- Header -->
    <div class="flex items-center justify-between">
      <div>
        <h3 class="text-lg font-medium">
          {{ $t('bots.subagents.title') }}
        </h3>
        <p class="text-sm text-muted-foreground">
          {{ $t('bots.subagents.subtitle') }}
        </p>
      </div>
      <Button
        size="sm"
        @click="handleCreate"
      >
        <FontAwesomeIcon
          :icon="['fas', 'plus']"
          class="mr-2"
        />
        {{ $t('bots.subagents.add') }}
      </Button>
    </div>

    <!-- Loading State -->
    <div
      v-if="isLoading"
      class="flex items-center justify-center py-8 text-sm text-muted-foreground"
    >
      <Spinner class="mr-2" />
      {{ $t('common.loading') }}
    </div>

    <!-- Empty State -->
    <div
      v-else-if="!subagents.length"
      class="flex flex-col items-center justify-center py-12 text-center"
    >
      <div class="rounded-full bg-muted p-3 mb-4">
        <FontAwesomeIcon
          :icon="['fas', 'robot']"
          class="size-6 text-muted-foreground"
        />
      </div>
      <h3 class="text-lg font-medium">
        {{ $t('bots.subagents.emptyTitle') }}
      </h3>
      <p class="text-sm text-muted-foreground mt-1">
        {{ $t('bots.subagents.emptyDescription') }}
      </p>
    </div>

    <!-- Subagents List -->
    <div
      v-else
      class="space-y-4"
    >
      <Card
        v-for="agent in subagents"
        :key="agent.id"
        class="flex flex-col"
      >
        <CardHeader class="pb-3">
          <div class="flex items-start justify-between gap-2">
            <div class="space-y-1 min-w-0">
              <CardTitle class="text-lg flex items-center gap-2">
                {{ agent.name }}
                <Badge
                  variant="outline"
                  class="font-normal text-xs font-mono"
                >
                  {{ agent.id }}
                </Badge>
              </CardTitle>
              <CardDescription>{{ agent.description || '-' }}</CardDescription>
            </div>
            <div class="flex items-center gap-1 shrink-0">
              <Button
                variant="outline"
                size="sm"
                class="mr-2"
                @click="handleViewContext(agent)"
              >
                <FontAwesomeIcon
                  :icon="['fas', 'eye']"
                  class="mr-2"
                />
                {{ $t('bots.subagents.viewContext') }}
                <Badge
                  v-if="agent.messages && agent.messages.length"
                  variant="secondary"
                  class="ml-1.5 text-[10px]"
                >
                  {{ agent.messages.length }}
                </Badge>
              </Button>
              <Button
                variant="ghost"
                size="sm"
                class="size-8 p-0"
                :title="$t('common.edit')"
                @click="handleEdit(agent)"
              >
                <FontAwesomeIcon
                  :icon="['fas', 'pen-to-square']"
                  class="size-3.5"
                />
              </Button>
              <ConfirmPopover
                :message="$t('bots.subagents.deleteConfirm')"
                :loading="isDeleting && deletingId === agent.id"
                @confirm="handleDelete(agent.id)"
              >
                <template #trigger>
                  <Button
                    variant="ghost"
                    size="sm"
                    class="size-8 p-0 text-destructive hover:text-destructive"
                    :disabled="isDeleting"
                    :title="$t('common.delete')"
                  >
                    <FontAwesomeIcon
                      :icon="['fas', 'trash']"
                      class="size-3.5"
                    />
                  </Button>
                </template>
              </ConfirmPopover>
            </div>
          </div>
        </CardHeader>
        <CardContent class="pb-4 space-y-3">
          <!-- Skills -->
          <div
            v-if="agent.skills && agent.skills.length > 0"
            class="space-y-1"
          >
            <span class="text-xs font-medium text-muted-foreground uppercase">{{ $t('bots.subagents.skills') }}</span>
            <div class="flex flex-wrap gap-1 mt-1">
              <Badge
                v-for="skill in agent.skills"
                :key="skill"
                variant="secondary"
              >
                {{ skill }}
              </Badge>
            </div>
          </div>

          <!-- Usage summary -->
          <div
            v-if="hasUsageData(agent.usage)"
            class="space-y-1"
          >
            <span class="text-xs font-medium text-muted-foreground uppercase">{{ $t('bots.subagents.usage') }}</span>
            <div class="flex flex-wrap items-center gap-3 mt-1 text-xs text-muted-foreground">
              <span
                v-for="(val, key) in flattenUsage(agent.usage)"
                :key="key"
                class="inline-flex items-center gap-1 bg-muted px-2 py-0.5 rounded"
              >
                <span class="font-medium">{{ key }}:</span> {{ val }}
              </span>
            </div>
          </div>

          <!-- Meta -->
          <div class="flex items-center gap-4 text-xs text-muted-foreground pt-2">
            <span v-if="agent.created_at">
              {{ $t('common.createdAt') }}: {{ formatDateTime(agent.created_at) }}
            </span>
            <span v-if="agent.messages">
              {{ $t('bots.subagents.messagesCount', { count: agent.messages.length }) }}
            </span>
          </div>
        </CardContent>
      </Card>
    </div>

    <!-- Edit Dialog -->
    <Dialog v-model:open="isDialogOpen">
      <DialogContent class="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{{ isEditing ? $t('common.edit') : $t('bots.subagents.add') }}</DialogTitle>
        </DialogHeader>
        <div class="space-y-4 py-4">
          <div class="space-y-2">
            <Label>{{ $t('common.name') }}</Label>
            <Input
              v-model="draftAgent.name"
              :placeholder="$t('common.namePlaceholder')"
              :disabled="isSaving"
            />
          </div>
          <div class="space-y-2">
            <Label>{{ $t('bots.subagents.description') }}</Label>
            <Textarea
              v-model="draftAgent.description"
              :placeholder="$t('bots.subagents.descriptionPlaceholder')"
              :disabled="isSaving"
              class="min-h-[100px]"
            />
          </div>
        </div>
        <DialogFooter>
          <DialogClose as-child>
            <Button
              variant="outline"
              :disabled="isSaving"
            >
              {{ $t('common.cancel') }}
            </Button>
          </DialogClose>
          <Button
            :disabled="!canSave || isSaving"
            @click="handleSave"
          >
            <Spinner
              v-if="isSaving"
              class="mr-2 size-4"
            />
            {{ $t('common.save') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- Context Dialog -->
    <Dialog v-model:open="isContextDialogOpen">
      <DialogContent class="max-w-3xl max-h-[80vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>
            {{ $t('bots.subagents.contextTitle') }}
            <Badge
              v-if="contextMessages.length"
              variant="secondary"
              class="ml-2 text-xs"
            >
              {{ $t('bots.subagents.messagesCount', { count: contextMessages.length }) }}
            </Badge>
          </DialogTitle>
        </DialogHeader>

        <!-- Usage info -->
        <div
          v-if="hasUsageData(contextUsage)"
          class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground"
        >
          <FontAwesomeIcon
            :icon="['fas', 'chart-bar']"
            class="size-3"
          />
          <span
            v-for="(val, key) in flattenUsage(contextUsage)"
            :key="key"
            class="bg-muted px-2 py-0.5 rounded"
          >
            {{ key }}: {{ val }}
          </span>
        </div>

        <!-- Messages list -->
        <ScrollArea class="flex-1 min-h-0 mt-2">
          <div
            v-if="contextMessages.length === 0"
            class="flex flex-col items-center justify-center py-8 text-center"
          >
            <p class="text-sm text-muted-foreground">
              {{ $t('bots.subagents.contextEmpty') }}
            </p>
          </div>
          <MessageList
            v-else
            :messages="contextMessages"
          />
        </ScrollArea>

        <DialogFooter class="pt-4">
          <DialogClose as-child>
            <Button variant="outline">
              {{ $t('common.cancel') }}
            </Button>
          </DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { toast } from 'vue-sonner'
import {
  Button, Card, CardHeader, CardTitle, CardDescription, CardContent,
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogClose,
  Input, Textarea, Label, Spinner, Badge, ScrollArea
} from '@memoh/ui'
import ConfirmPopover from '@/components/confirm-popover/index.vue'
import MessageList from './message-list.vue'
import {
  getBotsByBotIdSubagents,
  postBotsByBotIdSubagents,
  putBotsByBotIdSubagentsById,
  deleteBotsByBotIdSubagentsById,
  type SubagentSubagent
} from '@memoh/sdk'
import { resolveApiErrorMessage } from '@/utils/api-error'
import { formatDateTime } from '@/utils/date-time'

const props = defineProps<{
  botId: string
}>()

const { t } = useI18n()

const isLoading = ref(false)
const isSaving = ref(false)
const isDeleting = ref(false)
const deletingId = ref('')
const subagents = ref<SubagentSubagent[]>([])

const isDialogOpen = ref(false)
const isEditing = ref(false)
const draftAgent = ref<SubagentSubagent>({
  name: '',
  description: '',
})

const isContextDialogOpen = ref(false)
const contextMessages = ref<Array<Record<string, unknown>>>([])
const contextUsage = ref<Record<string, unknown>>({})

const canSave = computed(() => {
  return (draftAgent.value.name || '').trim() && (draftAgent.value.description || '').trim()
})

function hasUsageData(usage: unknown): boolean {
  if (!usage) return false
  if (typeof usage === 'object' && Object.keys(usage as object).length > 0) return true
  return false
}

function flattenUsage(usage: unknown): Record<string, unknown> {
  if (!usage || typeof usage !== 'object') return {}
  return usage as Record<string, unknown>
}

async function fetchSubagents() {
  if (!props.botId) return
  isLoading.value = true
  try {
    const { data } = await getBotsByBotIdSubagents({
      path: { bot_id: props.botId },
      throwOnError: true,
    })
    subagents.value = data.items || []
  } catch (error) {
    toast.error(resolveApiErrorMessage(error, t('bots.subagents.loadFailed')))
  } finally {
    isLoading.value = false
  }
}

function handleCreate() {
  isEditing.value = false
  draftAgent.value = {
    name: '',
    description: '',
  }
  isDialogOpen.value = true
}

function handleEdit(agent: SubagentSubagent) {
  isEditing.value = true
  draftAgent.value = {
    id: agent.id,
    name: agent.name || '',
    description: agent.description || '',
    metadata: agent.metadata,
  }
  isDialogOpen.value = true
}

async function handleSave() {
  if (!canSave.value) return
  isSaving.value = true
  try {
    if (isEditing.value && draftAgent.value.id) {
      await putBotsByBotIdSubagentsById({
        path: { bot_id: props.botId, id: draftAgent.value.id },
        body: {
          name: draftAgent.value.name?.trim(),
          description: draftAgent.value.description?.trim(),
          metadata: draftAgent.value.metadata,
        },
        throwOnError: true,
      })
    } else {
      await postBotsByBotIdSubagents({
        path: { bot_id: props.botId },
        body: {
          name: draftAgent.value.name?.trim(),
          description: draftAgent.value.description?.trim(),
          metadata: draftAgent.value.metadata,
          skills: [],
          messages: [],
        },
        throwOnError: true,
      })
    }
    toast.success(t('bots.subagents.saveSuccess'))
    isDialogOpen.value = false
    await fetchSubagents()
  } catch (error) {
    toast.error(resolveApiErrorMessage(error, t('bots.subagents.saveFailed')))
  } finally {
    isSaving.value = false
  }
}

async function handleDelete(id?: string) {
  if (!id) return
  isDeleting.value = true
  deletingId.value = id
  try {
    await deleteBotsByBotIdSubagentsById({
      path: { bot_id: props.botId, id },
      throwOnError: true,
    })
    toast.success(t('bots.subagents.deleteSuccess'))
    await fetchSubagents()
  } catch (error) {
    toast.error(resolveApiErrorMessage(error, t('bots.subagents.deleteFailed')))
  } finally {
    isDeleting.value = false
    deletingId.value = ''
  }
}

function handleViewContext(agent: SubagentSubagent) {
  const msgs = agent.messages || []
  contextMessages.value = msgs.map((m: Record<string, unknown>, idx: number) => ({
    id: String(idx),
    role: (m.role as string) || 'unknown',
    content: m.content,
    sender_display_name: m.sender_display_name as string | undefined,
    platform: m.platform as string | undefined,
    created_at: m.created_at as string | undefined,
    usage: m.usage,
    metadata: m.metadata as Record<string, unknown> | undefined,
  }))
  contextUsage.value = (agent.usage as Record<string, unknown>) || {}
  isContextDialogOpen.value = true
}

onMounted(() => {
  fetchSubagents()
})
</script>
