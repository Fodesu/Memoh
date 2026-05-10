<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { toast } from 'vue-sonner'
import {
  Button,
  Input,
  Label,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Textarea,
} from '@memohai/ui'
import { useQuery } from '@pinia/colada'
import { FileText, Upload } from 'lucide-vue-next'
import { getOrchestrationContainerImagesCapabilities, postOrchestrationContainerImages } from '@memohai/sdk'
import { resolveApiErrorMessage } from '@/utils/api-error'

const { t } = useI18n()
const router = useRouter()
const dockerfileInputRef = ref<HTMLInputElement | null>(null)

const form = reactive({
  name: '',
  imageType: 'registry',
  registrySource: 'dockerhub',
  image: '',
  description: '',
  dockerfile: 'FROM debian:bookworm-slim\n',
})

const { data: capabilities } = useQuery({
  key: () => ['orchestration-container-images-capabilities'],
  query: async () => {
    const { data } = await getOrchestrationContainerImagesCapabilities({ throwOnError: true })
    return data
  },
})

const dockerfileBuildSupported = computed(() => capabilities.value?.dockerfile_build === true)
const dockerfileBuildReason = computed(() => capabilities.value?.reason || t('orchestration.imageBuildUnsupported'))

const submitImageName = computed(() => {
  return form.name.trim()
})

const submitImageRef = computed(() => {
  return form.image.trim()
})

const canSubmit = computed(() => {
  if (!submitImageName.value || !submitImageRef.value) return false
  if (form.imageType !== 'dockerfile') return true
  if (!dockerfileBuildSupported.value) return false
  return Boolean(form.dockerfile.trim())
})

async function handleSubmit() {
  if (!canSubmit.value) return
  try {
    await postOrchestrationContainerImages({
      body: {
        name: submitImageName.value,
        source_type: form.imageType,
        image_ref: submitImageRef.value,
        dockerfile: form.imageType === 'dockerfile' ? form.dockerfile.trim() : '',
        build_options: {},
        metadata: {
          description: form.description.trim(),
          registry_source: form.registrySource,
        },
      },
      throwOnError: true,
    })
    toast.success(t('orchestration.imageAdded'))
    await router.push({ name: 'orchestration-images' })
  }
  catch (error) {
    toast.error(resolveApiErrorMessage(error, t('orchestration.imageAddFailed')))
  }
}

async function handleDockerfileFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  form.dockerfile = await file.text()
}

function openDockerfilePicker() {
  dockerfileInputRef.value?.click()
}
</script>

<template>
  <section class="mx-auto max-w-2xl p-4">
    <h2 class="mb-6 text-lg font-semibold">
      {{ $t('orchestration.addImage') }}
    </h2>

    <form @submit.prevent="handleSubmit">
      <div class="flex flex-col gap-4">
        <div>
          <Label class="mb-2">
            {{ $t('orchestration.imageSource') }}
            <span class="text-destructive">*</span>
          </Label>
          <Select v-model="form.registrySource">
            <SelectTrigger class="w-full">
              <SelectValue :placeholder="$t('orchestration.imageSource')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="dockerhub">
                {{ $t('orchestration.imageRegistryDockerHub') }}
              </SelectItem>
              <SelectItem value="ghcr">
                {{ $t('orchestration.imageRegistryGHCR') }}
              </SelectItem>
              <SelectItem value="quay">
                {{ $t('orchestration.imageRegistryQuay') }}
              </SelectItem>
              <SelectItem value="custom">
                {{ $t('orchestration.imageRegistryCustom') }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div>
          <Label class="mb-2">
            {{ $t('orchestration.imageType') }}
            <span class="text-destructive">*</span>
          </Label>
          <Select v-model="form.imageType">
            <SelectTrigger class="w-full">
              <SelectValue :placeholder="$t('orchestration.imageType')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="registry">
                {{ $t('orchestration.imageTypeRegistry') }}
              </SelectItem>
              <SelectItem
                value="dockerfile"
                :disabled="!dockerfileBuildSupported"
              >
                {{ $t('orchestration.imageTypeDockerfile') }}
              </SelectItem>
            </SelectContent>
          </Select>
          <p
            v-if="!dockerfileBuildSupported"
            class="mt-2 text-xs text-muted-foreground"
          >
            {{ dockerfileBuildReason }}
          </p>
        </div>

        <div>
          <Label class="mb-2">
            {{ $t('orchestration.imageName') }}
            <span class="text-destructive">*</span>
          </Label>
          <Input
            v-model="form.name"
            type="text"
            placeholder="Debian Bookworm"
          />
        </div>

        <div>
          <Label class="mb-2">
            {{ form.imageType === 'dockerfile' ? $t('orchestration.imageTag') : $t('orchestration.imageAddress') }}
            <span class="text-destructive">*</span>
          </Label>
          <Input
            v-model="form.image"
            type="text"
            placeholder="debian:bookworm-slim"
          />
        </div>

        <template v-if="form.imageType === 'dockerfile'">
          <div class="overflow-hidden rounded-xl border border-border/70 bg-background shadow-sm">
            <div class="flex items-center justify-between gap-3 border-b border-border/70 bg-muted/30 px-3 py-2">
              <div class="flex min-w-0 items-center gap-2">
                <div class="flex size-7 shrink-0 items-center justify-center rounded-md bg-background text-muted-foreground">
                  <FileText class="size-4" />
                </div>
                <div class="min-w-0">
                  <Label class="block truncate text-sm">
                    {{ $t('orchestration.dockerfile') }}
                    <span class="text-destructive">*</span>
                  </Label>
                </div>
              </div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                class="h-8 shrink-0 gap-1.5 px-2.5"
                :title="$t('orchestration.dockerfileFile')"
                @click="openDockerfilePicker"
              >
                <Upload class="size-4" />
                <span class="hidden sm:inline">{{ $t('orchestration.importDockerfile') }}</span>
              </Button>
              <input
                ref="dockerfileInputRef"
                type="file"
                accept=".dockerfile,Dockerfile,text/plain"
                class="sr-only"
                @change="handleDockerfileFile"
              >
            </div>
            <Textarea
              v-model="form.dockerfile"
              rows="12"
              class="min-h-72 resize-y rounded-none border-0 bg-background font-mono text-xs leading-5 shadow-none focus-visible:ring-0"
            />
          </div>
        </template>

        <div>
          <Label class="mb-2">
            {{ $t('orchestration.description') }}
          </Label>
          <Textarea
            v-model="form.description"
            rows="3"
          />
        </div>
      </div>

      <div class="mt-6 flex justify-end gap-3 pb-4">
        <Button
          type="button"
          variant="outline"
          @click="router.push({ name: 'orchestration-images' })"
        >
          {{ $t('common.cancel') }}
        </Button>
        <Button
          type="submit"
          :disabled="!canSubmit"
        >
          {{ $t('orchestration.addImage') }}
        </Button>
      </div>
    </form>
  </section>
</template>
