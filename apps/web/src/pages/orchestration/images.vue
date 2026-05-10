<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useQuery } from '@pinia/colada'
import { Badge, Button, Card, CardHeader, CardTitle, ScrollArea } from '@memohai/ui'
import { Container, LoaderCircle, Package, Plus } from 'lucide-vue-next'
import { getOrchestrationContainerImages } from '@memohai/sdk'
import { formatDate } from '@/utils/date-time'

const router = useRouter()
const { t } = useI18n()

const { data, asyncStatus } = useQuery({
  key: () => ['orchestration-container-images'],
  query: async () => {
    const { data } = await getOrchestrationContainerImages({ throwOnError: true })
    return data
  },
})

const images = computed(() => data.value?.items ?? [])
const sortedImages = computed(() => [
  ...images.value.filter((image) => image.builtin),
  ...images.value.filter((image) => !image.builtin),
])

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

function imageCreatedAt(image: { builtin?: boolean, created_at?: string }) {
  if (image.builtin || !image.created_at) return ''
  return formatDate(image.created_at)
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col bg-background">
    <header class="flex h-14 shrink-0 items-center justify-between gap-3 border-b border-border/70 px-5">
      <div class="flex min-w-0 items-center gap-3">
        <div class="flex size-8 items-center justify-center rounded-lg border border-border bg-card text-foreground shadow-sm">
          <Container class="size-4" />
        </div>
        <h1 class="truncate text-sm font-semibold">
          {{ $t('orchestration.images') }}
        </h1>
      </div>
      <Button
        class="h-8 gap-1.5 px-3 text-xs"
        @click="router.push({ name: 'orchestration-images-new' })"
      >
        <Plus class="size-3.5" />
        {{ $t('orchestration.addImage') }}
      </Button>
    </header>

    <ScrollArea class="min-h-0 flex-1">
      <main class="p-5">
        <div
          v-if="asyncStatus === 'loading'"
          class="flex items-center justify-center rounded-xl border border-dashed border-border/70 py-20 text-sm text-muted-foreground"
        >
          <LoaderCircle class="mr-2 size-4 animate-spin" />
          {{ $t('orchestration.loadingImages') }}
        </div>

        <div
          v-else
          class="grid gap-3 lg:grid-cols-2 xl:grid-cols-3"
        >
          <Card
            v-for="image in sortedImages"
            :key="image.id"
            class="h-full cursor-pointer transition-shadow hover:shadow-md"
            role="button"
            tabindex="0"
            @click="router.push({ name: 'orchestration-image-detail', params: { id: image.id } })"
            @keydown.enter.prevent="router.push({ name: 'orchestration-image-detail', params: { id: image.id } })"
            @keydown.space.prevent="router.push({ name: 'orchestration-image-detail', params: { id: image.id } })"
          >
            <CardHeader class="flex flex-row items-start gap-3 space-y-0 pb-2">
              <div class="flex size-11 shrink-0 items-center justify-center rounded-full bg-muted text-foreground">
                <Package class="size-5" />
              </div>
              <div class="flex min-w-0 flex-1 flex-col gap-1.5">
                <div class="flex items-center justify-between gap-2">
                  <CardTitle class="truncate text-sm">
                    {{ image.name }}
                  </CardTitle>
                  <Badge
                    :variant="imageStatusVariant(image.status)"
                    class="shrink-0 text-xs"
                  >
                    {{ imageStatusLabel(image.status) }}
                  </Badge>
                </div>
                <div class="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-muted-foreground">
                  <span v-if="imageCreatedAt(image)">
                    {{ $t('common.createdAt') }} {{ imageCreatedAt(image) }}
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
