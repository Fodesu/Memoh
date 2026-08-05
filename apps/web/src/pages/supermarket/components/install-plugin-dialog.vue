<template>
  <Dialog
    :open="open"
    @update:open="$emit('update:open', $event)"
  >
    <DialogContent class="sm:max-w-lg">
      <DialogHeader>
        <DialogTitle>{{ $t('supermarket.pluginInstallTitle') }}</DialogTitle>
      </DialogHeader>
      <div class="space-y-4 py-2">
        <FieldStack :label="$t('supermarket.selectBot')">
          <BotSelect
            v-model="selectedBotId"
            trigger-class="w-full"
          />
        </FieldStack>

        <div
          v-if="plugin"
          class="rounded-md border border-border p-3 space-y-1"
        >
          <div class="flex min-w-0 flex-wrap items-center gap-2">
            <p
              class="min-w-0 truncate text-xs font-medium"
              :title="plugin.name"
            >
              {{ plugin.name }}
            </p>
          </div>
          <p class="text-caption text-muted-foreground line-clamp-3">
            {{ plugin.description }}
          </p>
          <div
            v-if="pluginPackages.length"
            class="mt-3 grid gap-1.5"
          >
            <div
              v-for="pkg in pluginPackages"
              :key="packageKey(pkg)"
              class="flex min-w-0 items-start gap-2 rounded border border-border-soft bg-muted/20 px-2 py-1.5"
            >
              <Boxes class="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
              <div class="min-w-0 flex-1">
                <p
                  class="truncate text-caption font-medium"
                  :title="pkg.package_id"
                >
                  {{ pkg.package_id }}
                </p>
                <p
                  class="truncate text-[10px] text-muted-foreground"
                  :title="pkg.registry_id"
                >
                  {{ pkg.registry_id }}
                </p>
              </div>
            </div>
          </div>
        </div>
      </div>
      <DialogFooter>
        <DialogClose as-child>
          <Button
            variant="outline"
            :disabled="installing"
          >
            {{ $t('common.cancel') }}
          </Button>
        </DialogClose>
        <Button
          :disabled="!canInstall"
          :loading="installing"
          @click="handleInstall"
        >
          {{ installing ? $t('supermarket.installing') : $t('supermarket.install') }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { Boxes } from 'lucide-vue-next'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogClose,
  Button, toast,
} from '@felinic/ui'
import {
  getBotsByBotIdPlugins,
  postBotsByBotIdSupermarketInstallPlugin,
  type HandlersSupermarketPluginEntry,
  type HandlersSupermarketPluginResolvedPackage,
} from '@memohai/sdk'
import { FieldStack } from '@felinic/ui'
import { resolveApiErrorMessage } from '@/utils/api-error'
import { emitBotPluginsUpdated } from '@/utils/bot-plugin-events'
import BotSelect from '@/components/bot-select/index.vue'

const props = defineProps<{
  open: boolean
  plugin: HandlersSupermarketPluginEntry | null
}>()

const emit = defineEmits<{
  'update:open': [open: boolean]
  'installed': []
}>()

const { t } = useI18n()
const router = useRouter()

const selectedBotId = ref('')
const installing = ref(false)

const pluginPackages = computed<HandlersSupermarketPluginResolvedPackage[]>(() => props.plugin?.release.packages ?? [])

const canInstall = computed(() => {
  return Boolean(selectedBotId.value && props.plugin?.id && props.plugin.release?.revision)
})

function packageKey(pkg: HandlersSupermarketPluginResolvedPackage): string {
  return `${pkg.registry_id}/${pkg.package_id}`
}

watch(() => props.open, (open) => {
  if (!open) {
    selectedBotId.value = ''
    installing.value = false
  }
})

async function handleInstall() {
  if (!selectedBotId.value || !props.plugin?.id || !props.plugin.release?.revision) return
  const botId = selectedBotId.value
  installing.value = true
  try {
    const { data: installedPlugins } = await getBotsByBotIdPlugins({
      path: { bot_id: botId },
      throwOnError: true,
    })
    const installedPlugin = (installedPlugins.items ?? []).find(item => item.plugin_id === props.plugin?.id)
    const installedRevision = installedPlugin?.metadata?.release_revision
    const expectedInstalledRevision = typeof installedRevision === 'string' && installedRevision
      ? installedRevision
      : null
    const expectedInstallationUpdatedAt = expectedInstalledRevision && installedPlugin?.updated_at
      ? installedPlugin.updated_at
      : null
    await postBotsByBotIdSupermarketInstallPlugin({
      path: { bot_id: botId },
      body: {
        plugin_id: props.plugin.id,
        release_revision: props.plugin.release.revision,
        expected_installed_revision: expectedInstalledRevision,
        expected_installation_updated_at: expectedInstallationUpdatedAt,
      },
      throwOnError: true,
    })
    toast.success(t('supermarket.installSuccess'))
    emitBotPluginsUpdated(botId)
    emit('update:open', false)
    emit('installed')
    void router.push({
      name: 'bot-detail',
      params: { botName: botId },
      query: { tab: 'plugins' },
    }).catch(() => {})
  } catch (error) {
    toast.error(resolveApiErrorMessage(error, t('supermarket.installFailed')))
  } finally {
    installing.value = false
  }
}
</script>
