<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { useQuery } from '@pinia/colada'
import { Badge, Button, Card, CardContent, CardHeader, CardTitle, ScrollArea, Textarea } from '@memohai/ui'
import { ArrowLeft, LoaderCircle, Package } from 'lucide-vue-next'
import { getOrchestrationContainerImages, type HandlersContainerImageView } from '@memohai/sdk'
import { formatDate } from '@/utils/date-time'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const imageId = computed(() => String(route.params.id ?? ''))

const { data, asyncStatus } = useQuery({
  key: () => ['orchestration-container-images'],
  query: async () => {
    const { data } = await getOrchestrationContainerImages({ throwOnError: true })
    return data
  },
})

const image = computed(() =>
  (data.value?.items ?? []).find((item) => item.id === imageId.value),
)

const detailRows = computed(() => {
  const current = image.value
  if (!current) return []
  return [
    { label: t('orchestration.imageName'), value: current.name || '--' },
    { label: t('orchestration.imageTag'), value: current.image_ref || '--', mono: true },
    { label: t('orchestration.imageType'), value: imageTypeLabel(current.source_type) },
    { label: t('orchestration.imageSource'), value: imageSourceLabel(current) },
    { label: t('common.createdAt'), value: current.created_at ? formatDate(current.created_at) : '--' },
    { label: t('orchestration.updatedAt'), value: current.updated_at ? formatDate(current.updated_at) : '--' },
    { label: t('orchestration.imageDigest'), value: current.digest || '--', mono: true },
    { label: t('orchestration.defaultImage'), value: current.builtin ? t('common.yes') : t('common.no') },
  ]
})

function imageTypeLabel(type?: string) {
  return type === 'dockerfile' ? t('orchestration.imageTypeDockerfile') : t('orchestration.imageTypeRegistry')
}

function imageSourceLabel(current: HandlersContainerImageView) {
  switch (current.metadata?.registry_source) {
    case 'ghcr':
      return t('orchestration.imageRegistryGHCR')
    case 'quay':
      return t('orchestration.imageRegistryQuay')
    case 'custom':
      return t('orchestration.imageRegistryCustom')
    case 'dockerhub':
    default:
      return t('orchestration.imageRegistryDockerHub')
  }
}

function imageStatusLabel(status?: string) {
  switch (status) {
    case 'pending':
      return t('orchestration.imageStatusPending')
    case 'building':
      return t('orchestration.imageStatusBuilding')
    case 'failed':
      return t('orchestration.imageStatusFailed')
    case 'archived':
      return t('orchestration.statusArchived')
    default:
      return t('orchestration.imageStatusReady')
  }
}

function imageStatusVariant(status?: string) {
  switch (status) {
    case 'ready':
      return 'default'
    case 'failed':
      return 'destructive'
    case 'pending':
    case 'building':
      return 'secondary'
    case 'archived':
      return 'outline'
    default:
      return 'secondary'
  }
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col bg-background">
    <header class="flex h-14 shrink-0 items-center justify-between gap-3 border-b border-border/70 px-5">
      <div class="flex min-w-0 items-center gap-3">
        <Button
          variant="ghost"
          size="icon"
          class="size-8"
          @click="router.push({ name: 'orchestration-images' })"
        >
          <ArrowLeft class="size-4" />
        </Button>
        <div class="flex size-8 items-center justify-center rounded-lg bg-muted text-foreground">
          <Package class="size-4" />
        </div>
        <h1 class="truncate text-sm font-semibold">
          {{ image?.name || $t('orchestration.imageDetail') }}
        </h1>
      </div>
      <Badge
        v-if="image"
        :variant="imageStatusVariant(image.status)"
        class="shrink-0 text-xs"
      >
        {{ imageStatusLabel(image.status) }}
      </Badge>
    </header>

    <ScrollArea class="min-h-0 flex-1">
      <main class="mx-auto w-full max-w-4xl p-5">
        <div
          v-if="asyncStatus === 'loading'"
          class="flex items-center justify-center rounded-xl border border-dashed border-border/70 py-20 text-sm text-muted-foreground"
        >
          <LoaderCircle class="mr-2 size-4 animate-spin" />
          {{ $t('orchestration.loadingImages') }}
        </div>

        <div
          v-else-if="!image"
          class="rounded-xl border border-dashed border-border/70 py-20 text-center text-sm text-muted-foreground"
        >
          {{ $t('orchestration.imageNotFound') }}
        </div>

        <div
          v-else
          class="space-y-4"
        >
          <Card>
            <CardHeader>
              <CardTitle class="text-sm">
                {{ $t('orchestration.imageDetail') }}
              </CardTitle>
            </CardHeader>
            <CardContent>
              <dl class="grid gap-3 text-sm md:grid-cols-2">
                <div
                  v-for="row in detailRows"
                  :key="row.label"
                  class="rounded-lg bg-muted/30 px-3 py-2"
                >
                  <dt class="text-xs text-muted-foreground">
                    {{ row.label }}
                  </dt>
                  <dd
                    class="mt-1 break-all"
                    :class="row.mono ? 'font-mono text-xs' : 'font-medium'"
                  >
                    {{ row.value }}
                  </dd>
                </div>
              </dl>
            </CardContent>
          </Card>

          <Card v-if="image.last_build_error">
            <CardHeader>
              <CardTitle class="text-sm">
                {{ $t('orchestration.imageBuildError') }}
              </CardTitle>
            </CardHeader>
            <CardContent>
              <pre class="whitespace-pre-wrap rounded-lg bg-muted/30 p-3 font-mono text-xs text-destructive">{{ image.last_build_error }}</pre>
            </CardContent>
          </Card>

          <Card v-if="image.source_type === 'dockerfile' && image.dockerfile">
            <CardHeader>
              <CardTitle class="text-sm">
                {{ $t('orchestration.dockerfile') }}
              </CardTitle>
            </CardHeader>
            <CardContent>
              <Textarea
                :model-value="image.dockerfile"
                readonly
                rows="12"
                class="min-h-72 resize-y font-mono text-xs"
              />
            </CardContent>
          </Card>
        </div>
      </main>
    </ScrollArea>
  </div>
</template>
