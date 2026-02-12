<template>
  <div class="max-w-4xl mx-auto space-y-5">
    <div class="flex items-start justify-between gap-3">
      <div class="space-y-1 min-w-0">
        <h3 class="text-lg font-semibold">
          {{ $t('mcp.addTitle') }}
        </h3>
        <p class="text-sm text-muted-foreground">
          {{ $t('mcp.addDescription') }}
        </p>
      </div>
      <div class="flex flex-wrap gap-2 shrink-0 justify-end">
        <Button
          variant="outline"
          size="sm"
          :disabled="loading"
          @click="loadList"
        >
          <Spinner
            v-if="loading"
            class="mr-1.5"
          />
          {{ $t('bots.container.actions.refresh') }}
        </Button>
        <Button
          variant="outline"
          size="sm"
          @click="handleExport"
        >
          {{ $t('common.export') }}
        </Button>
        <Button
          variant="outline"
          size="sm"
          @click="importDialogOpen = true"
        >
          {{ $t('common.import') }}
        </Button>
        <Button
          size="sm"
          @click="openCreateDialog"
        >
          {{ $t('common.add') }}
        </Button>
      </div>
    </div>

    <!-- Loading -->
    <div
      v-if="loading && items.length === 0"
      class="flex items-center gap-2 text-sm text-muted-foreground"
    >
      <Spinner />
      <span>{{ $t('common.loading') }}</span>
    </div>

    <!-- Empty -->
    <div
      v-else-if="items.length === 0"
      class="rounded-md border p-4"
    >
      <p class="text-sm text-muted-foreground">
        {{ $t('mcp.empty') }}
      </p>
    </div>

    <!-- Table -->
    <DataTable
      v-else
      :columns="columns"
      :data="items"
    />

    <!-- Create/Edit dialog -->
    <Dialog v-model:open="formDialogOpen">
      <DialogContent class="sm:max-w-lg">
        <form @submit.prevent="handleSubmit">
          <DialogHeader>
            <DialogTitle>{{ editingItem ? $t('common.edit') : $t('common.add') }} MCP Server</DialogTitle>
          </DialogHeader>

          <div class="mt-4 flex flex-col gap-3">
            <div class="space-y-1.5">
              <Label>{{ $t('mcp.name') }}</Label>
              <Input
                v-model="formData.name"
                :placeholder="$t('mcp.namePlaceholder')"
              />
            </div>

            <div class="space-y-1.5">
              <Label>{{ $t('mcp.command') }} <span class="text-muted-foreground text-xs">({{ $t('common.optional') }})</span></Label>
              <Input
                v-model="formData.command"
                :placeholder="$t('mcp.commandPlaceholder')"
                :disabled="!!formData.url"
              />
            </div>

            <div class="space-y-1.5">
              <Label>URL <span class="text-muted-foreground text-xs">({{ $t('common.optional') }})</span></Label>
              <Input
                v-model="formData.url"
                placeholder="https://example.com/mcp"
                :disabled="!!formData.command"
              />
            </div>

            <div
              v-if="formData.command"
              class="space-y-1.5"
            >
              <Label>{{ $t('mcp.arguments') }}</Label>
              <TagsInput
                v-model="argsTags"
                :add-on-blur="true"
                :duplicate="true"
              >
                <TagsInputItem
                  v-for="item in argsTags"
                  :key="item"
                  :value="item"
                >
                  <TagsInputItemText />
                  <TagsInputItemDelete />
                </TagsInputItem>
                <TagsInputInput
                  :placeholder="$t('mcp.argumentsPlaceholder')"
                  class="w-full py-1"
                />
              </TagsInput>
            </div>

            <div
              v-if="formData.command"
              class="space-y-1.5"
            >
              <Label>{{ $t('mcp.env') }}</Label>
              <TagsInput
                :model-value="envTags.tagList.value"
                :add-on-blur="true"
                :convert-value="envTags.convertValue"
                @update:model-value="(tags) => envTags.handleUpdate(tags.map(String))"
              >
                <TagsInputItem
                  v-for="(value, index) in envTags.tagList.value"
                  :key="index"
                  :value="value"
                >
                  <TagsInputItemText />
                  <TagsInputItemDelete />
                </TagsInputItem>
                <TagsInputInput
                  :placeholder="$t('mcp.envPlaceholder')"
                  class="w-full py-1"
                />
              </TagsInput>
            </div>

            <div
              v-if="formData.command"
              class="space-y-1.5"
            >
              <Label>{{ $t('mcp.cwd') }}</Label>
              <Input
                v-model="formData.cwd"
                :placeholder="$t('mcp.cwdPlaceholder')"
              />
            </div>

            <div
              v-if="formData.url"
              class="space-y-1.5"
            >
              <Label>Headers</Label>
              <TagsInput
                :model-value="headerTags.tagList.value"
                :add-on-blur="true"
                :convert-value="headerTags.convertValue"
                @update:model-value="(tags) => headerTags.handleUpdate(tags.map(String))"
              >
                <TagsInputItem
                  v-for="(value, index) in headerTags.tagList.value"
                  :key="index"
                  :value="value"
                >
                  <TagsInputItemText />
                  <TagsInputItemDelete />
                </TagsInputItem>
                <TagsInputInput
                  placeholder="Key:Value"
                  class="w-full py-1"
                />
              </TagsInput>
            </div>

            <div
              v-if="formData.url && !formData.command"
              class="space-y-1.5"
            >
              <Label>Transport</Label>
              <Select v-model="formData.transport">
                <SelectTrigger class="w-full">
                  <SelectValue placeholder="http" />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="http">
                      HTTP (Streamable)
                    </SelectItem>
                    <SelectItem value="sse">
                      SSE
                    </SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>

            <div class="flex items-center gap-3">
              <Label>{{ $t('mcp.active') }}</Label>
              <Switch v-model:checked="formData.active" />
            </div>
          </div>

          <DialogFooter class="mt-6">
            <DialogClose as-child>
              <Button variant="outline">
                {{ $t('common.cancel') }}
              </Button>
            </DialogClose>
            <Button
              type="submit"
              :disabled="submitting || !formData.name.trim() || (!formData.command.trim() && !formData.url.trim())"
            >
              <Spinner
                v-if="submitting"
                class="mr-1.5"
              />
              {{ $t('common.confirm') }}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>

    <!-- Import dialog -->
    <Dialog v-model:open="importDialogOpen">
      <DialogContent class="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ $t('common.import') }} mcpServers</DialogTitle>
        </DialogHeader>
        <div class="mt-4 space-y-3">
          <p class="text-sm text-muted-foreground">
            {{ $t('mcp.importHint') }}
          </p>
          <Textarea
            v-model="importJson"
            rows="10"
            class="font-mono text-xs"
            placeholder='{ "mcpServers": { "filesystem": { "command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem"] } } }'
          />
        </div>
        <DialogFooter class="mt-4">
          <DialogClose as-child>
            <Button variant="outline">
              {{ $t('common.cancel') }}
            </Button>
          </DialogClose>
          <Button
            :disabled="importSubmitting || !importJson.trim()"
            @click="handleImport"
          >
            <Spinner
              v-if="importSubmitting"
              class="mr-1.5"
            />
            {{ $t('common.import') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- Export dialog -->
    <Dialog v-model:open="exportDialogOpen">
      <DialogContent class="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ $t('common.export') }} mcpServers</DialogTitle>
        </DialogHeader>
        <div class="mt-4">
          <Textarea
            :model-value="exportJson"
            rows="10"
            class="font-mono text-xs"
            readonly
          />
        </div>
        <DialogFooter class="mt-4">
          <Button
            variant="outline"
            @click="handleCopyExport"
          >
            {{ $t('common.copy') }}
          </Button>
          <DialogClose as-child>
            <Button>
              {{ $t('common.confirm') }}
            </Button>
          </DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { h, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { toast } from 'vue-sonner'
import { type ColumnDef } from '@tanstack/vue-table'
import {
  Badge,
  Button,
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Input,
  Label,
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Spinner,
  Switch,
  TagsInput,
  TagsInputInput,
  TagsInputItem,
  TagsInputItemDelete,
  TagsInputItemText,
  Textarea,
} from '@memoh/ui'
import DataTable from '@/components/data-table/index.vue'
import { useKeyValueTags } from '@/composables/useKeyValueTags'
import { client } from '@memoh/sdk/client'
import ConfirmPopover from '@/components/confirm-popover/index.vue'

interface McpItem {
  id: string
  name: string
  type: string
  config: Record<string, unknown>
  is_active: boolean
}

const props = defineProps<{ botId: string }>()
const { t } = useI18n()

const loading = ref(false)
const items = ref<McpItem[]>([])
const formDialogOpen = ref(false)
const editingItem = ref<McpItem | null>(null)
const submitting = ref(false)
const importDialogOpen = ref(false)
const importJson = ref('')
const importSubmitting = ref(false)
const exportDialogOpen = ref(false)
const exportJson = ref('')

const formData = ref({
  name: '',
  command: '',
  url: '',
  cwd: '',
  transport: 'http',
  active: true,
})
const argsTags = ref<string[]>([])
const envTags = useKeyValueTags()
const headerTags = useKeyValueTags()

function configValue(config: Record<string, unknown>, key: string): string {
  const val = config?.[key]
  return typeof val === 'string' ? val : ''
}

function configArray(config: Record<string, unknown>, key: string): string[] {
  const val = config?.[key]
  if (Array.isArray(val)) return val.map(String)
  return []
}

function configMap(config: Record<string, unknown>, key: string): Record<string, string> {
  const val = config?.[key]
  if (val && typeof val === 'object' && !Array.isArray(val)) {
    const out: Record<string, string> = {}
    for (const [k, v] of Object.entries(val)) {
      out[k] = String(v)
    }
    return out
  }
  return {}
}

const columns: ColumnDef<McpItem>[] = [
  {
    accessorKey: 'name',
    header: () => h('div', { class: 'text-left py-4' }, t('mcp.table.name')),
  },
  {
    accessorKey: 'type',
    header: () => h('div', { class: 'text-left' }, t('mcp.table.type')),
    cell: ({ row }) => h(Badge, { variant: 'outline' }, () => row.original.type),
  },
  {
    id: 'target',
    header: () => h('div', { class: 'text-left' }, 'Command / URL'),
    cell: ({ row }) => {
      const cfg = row.original.config ?? {}
      return h('span', { class: 'font-mono text-xs' },
        configValue(cfg, 'command') || configValue(cfg, 'url') || '-',
      )
    },
  },
  {
    id: 'status',
    header: () => h('div', { class: 'text-center' }, t('mcp.active')),
    cell: ({ row }) => h('div', { class: 'text-center' },
      h(Badge, { variant: row.original.is_active ? 'default' : 'secondary' },
        () => row.original.is_active ? 'ON' : 'OFF'),
    ),
  },
  {
    id: 'actions',
    header: () => h('div', { class: 'text-center' }, t('common.operation')),
    cell: ({ row }) => h('div', { class: 'flex gap-2 justify-center' }, [
      h(Button, {
        size: 'sm',
        variant: 'outline',
        onClick: () => openEditDialog(row.original),
      }, () => t('common.edit')),
      h(ConfirmPopover, {
        message: t('mcp.deleteConfirm'),
        onConfirm: () => handleDelete(row.original.id),
      }, {
        trigger: () => h(Button, {
          size: 'sm',
          variant: 'destructive',
        }, () => t('common.delete')),
      }),
    ]),
  },
]

async function loadList() {
  loading.value = true
  try {
    const { data } = await client.get({
      url: `/bots/${props.botId}/mcp`,
      throwOnError: true,
    }) as { data: { items: McpItem[] } }
    items.value = data.items ?? []
  } catch (error) {
    toast.error(resolveError(error, t('mcp.loadFailed')))
  } finally {
    loading.value = false
  }
}

function openCreateDialog() {
  editingItem.value = null
  formData.value = { name: '', command: '', url: '', cwd: '', transport: 'http', active: true }
  argsTags.value = []
  envTags.initFromObject(null)
  headerTags.initFromObject(null)
  formDialogOpen.value = true
}

function openEditDialog(item: McpItem) {
  editingItem.value = item
  const cfg = item.config ?? {}
  formData.value = {
    name: item.name,
    command: configValue(cfg, 'command'),
    url: configValue(cfg, 'url'),
    cwd: configValue(cfg, 'cwd'),
    transport: item.type === 'sse' ? 'sse' : 'http',
    active: item.is_active,
  }
  argsTags.value = configArray(cfg, 'args')
  envTags.initFromObject(configMap(cfg, 'env'))
  headerTags.initFromObject(configMap(cfg, 'headers'))
  formDialogOpen.value = true
}

function buildRequestBody() {
  const body: Record<string, unknown> = {
    name: formData.value.name.trim(),
    is_active: formData.value.active,
  }
  if (formData.value.command.trim()) {
    body.command = formData.value.command.trim()
    if (argsTags.value.length > 0) body.args = argsTags.value
    const env: Record<string, string> = {}
    envTags.tagList.value.forEach((tag) => {
      const [k, v] = tag.split(':')
      if (k && v) env[k] = v
    })
    if (Object.keys(env).length > 0) body.env = env
    if (formData.value.cwd.trim()) body.cwd = formData.value.cwd.trim()
  } else if (formData.value.url.trim()) {
    body.url = formData.value.url.trim()
    const headers: Record<string, string> = {}
    headerTags.tagList.value.forEach((tag) => {
      const [k, v] = tag.split(':')
      if (k && v) headers[k] = v
    })
    if (Object.keys(headers).length > 0) body.headers = headers
    if (formData.value.transport === 'sse') body.transport = 'sse'
  }
  return body
}

async function handleSubmit() {
  submitting.value = true
  try {
    const body = buildRequestBody()
    if (editingItem.value) {
      await client.put({
        url: `/bots/${props.botId}/mcp/${editingItem.value.id}`,
        body,
        throwOnError: true,
      })
    } else {
      await client.post({
        url: `/bots/${props.botId}/mcp`,
        body,
        throwOnError: true,
      })
    }
    formDialogOpen.value = false
    await loadList()
    toast.success(editingItem.value ? t('mcp.updateSuccess') : t('mcp.createSuccess'))
  } catch (error) {
    toast.error(resolveError(error, t('mcp.saveFailed')))
  } finally {
    submitting.value = false
  }
}

async function handleDelete(id: string) {
  try {
    await client.delete({
      url: `/bots/${props.botId}/mcp/${id}`,
      throwOnError: true,
    })
    await loadList()
    toast.success(t('mcp.deleteSuccess'))
  } catch (error) {
    toast.error(resolveError(error, t('mcp.deleteFailed')))
  }
}

async function handleImport() {
  importSubmitting.value = true
  try {
    let parsed = JSON.parse(importJson.value)
    if (!parsed.mcpServers && typeof parsed === 'object') {
      parsed = { mcpServers: parsed }
    }
    await client.put({
      url: `/bots/${props.botId}/mcp/import`,
      body: parsed,
      throwOnError: true,
    })
    importDialogOpen.value = false
    importJson.value = ''
    await loadList()
    toast.success(t('mcp.importSuccess'))
  } catch (error) {
    toast.error(resolveError(error, t('mcp.importFailed')))
  } finally {
    importSubmitting.value = false
  }
}

async function handleExport() {
  try {
    const { data } = await client.get({
      url: `/bots/${props.botId}/mcp/export`,
      throwOnError: true,
    }) as { data: { mcpServers: Record<string, unknown> } }
    exportJson.value = JSON.stringify(data, null, 2)
    exportDialogOpen.value = true
  } catch (error) {
    toast.error(resolveError(error, t('mcp.exportFailed')))
  }
}

function handleCopyExport() {
  navigator.clipboard.writeText(exportJson.value)
  toast.success(t('common.copied'))
}

function resolveError(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message.trim()) return error.message
  if (error && typeof error === 'object' && 'message' in error) {
    const msg = (error as { message?: string }).message
    if (msg?.trim()) return msg
  }
  return fallback
}

watch(() => props.botId, () => {
  if (props.botId) loadList()
}, { immediate: true })
</script>
