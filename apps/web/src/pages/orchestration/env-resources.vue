<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useQuery } from '@pinia/colada'
import { Badge, Button, Card, CardHeader, CardTitle, ScrollArea } from '@memohai/ui'
import { Boxes, Chrome, Container, LoaderCircle, Plus, RefreshCw, Server } from 'lucide-vue-next'
import {
  getOrchestrationEnvResources,
  type HandlersEnvResourceView,
} from '@memohai/sdk'
import { formatDate } from '@/utils/date-time'

const { t } = useI18n()
const router = useRouter()

const { data, asyncStatus, refetch } = useQuery({
  key: () => ['orchestration-env-resources'],
  query: async () => {
    const { data } = await getOrchestrationEnvResources({ throwOnError: true })
    return data
  },
})

const resources = computed(() => data.value?.items ?? [])

function resourceIcon(resource: HandlersEnvResourceView) {
  return resource.kind === 'browser' ? Chrome : Container
}

function resourceStatusLabel(resource: HandlersEnvResourceView) {
  switch (resource.status) {
    case 'active':
      return t('orchestration.statusActive')
    case 'disabled':
      return t('orchestration.statusDisabled')
    case 'archived':
      return t('orchestration.statusArchived')
    default:
      return resource.status || '--'
  }
}

function resourceStatusVariant(resource: HandlersEnvResourceView) {
  switch (resource.status) {
    case 'active':
      return 'default'
    case 'disabled':
      return 'secondary'
    case 'archived':
      return 'outline'
    default:
      return 'secondary'
  }
}

function resourceCreatedAt(resource: HandlersEnvResourceView) {
  return resource.created_at ? formatDate(resource.created_at) : ''
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col bg-background">
    <header class="flex h-14 shrink-0 items-center justify-between gap-3 border-b border-border/70 px-5">
      <div class="flex min-w-0 items-center gap-3">
        <div class="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
          <Server class="size-4" />
        </div>
        <h1 class="truncate text-sm font-semibold">
          {{ $t('orchestration.envResources') }}
        </h1>
      </div>
      <div class="flex items-center gap-2">
        <Button
          variant="outline"
          size="icon"
          class="size-8"
          :title="$t('orchestration.refresh')"
          @click="refetch()"
        >
          <RefreshCw class="size-3.5" />
        </Button>
        <Button
          class="h-8 gap-1.5 px-3 text-xs"
          @click="router.push({ name: 'orchestration-env-resources-new' })"
        >
          <Plus class="size-3.5" />
          {{ $t('orchestration.addEnv') }}
        </Button>
      </div>
    </header>

    <ScrollArea class="min-h-0 flex-1">
      <main class="p-5">
        <div
          v-if="asyncStatus === 'loading'"
          class="flex items-center justify-center rounded-xl border border-dashed border-border/70 py-20 text-sm text-muted-foreground"
        >
          <LoaderCircle class="mr-2 size-4 animate-spin" />
          {{ $t('orchestration.loadingEnvResources') }}
        </div>

        <div
          v-else-if="resources.length === 0"
          class="flex flex-col items-center justify-center rounded-xl border border-dashed border-border/70 bg-muted/15 py-20 text-center"
        >
          <Boxes class="mb-3 size-8 text-muted-foreground" />
          <p class="text-sm font-medium">
            {{ $t('orchestration.noEnvResources') }}
          </p>
        </div>

        <div
          v-else
          class="grid gap-3 lg:grid-cols-2 xl:grid-cols-3"
        >
          <Card
            v-for="resource in resources"
            :key="resource.id"
            class="h-full cursor-pointer transition-shadow hover:shadow-md"
            role="button"
            tabindex="0"
            @click="router.push({ name: 'orchestration-env-resource-detail', params: { id: resource.id } })"
            @keydown.enter.prevent="router.push({ name: 'orchestration-env-resource-detail', params: { id: resource.id } })"
            @keydown.space.prevent="router.push({ name: 'orchestration-env-resource-detail', params: { id: resource.id } })"
          >
            <CardHeader class="flex flex-row items-start gap-3 space-y-0 pb-2">
              <div class="flex size-11 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
                <component
                  :is="resourceIcon(resource)"
                  class="size-5"
                />
              </div>
              <div class="flex min-w-0 flex-1 flex-col gap-1.5">
                <div class="flex items-center justify-between gap-2">
                  <CardTitle class="truncate text-sm">
                    {{ resource.name || '--' }}
                  </CardTitle>
                  <Badge
                    :variant="resourceStatusVariant(resource)"
                    class="shrink-0 text-xs"
                  >
                    {{ resourceStatusLabel(resource) }}
                  </Badge>
                </div>
                <div class="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-muted-foreground">
                  <span v-if="resourceCreatedAt(resource)">
                    {{ $t('common.createdAt') }} {{ resourceCreatedAt(resource) }}
                  </span>
                </div>
              </div>
            </CardHeader>
          </Card>
        </div>
      </main>
    </ScrollArea>
  </div>
</template>
