<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'
import { useQuery, useQueryCache } from '@pinia/colada'
import {
  Button,
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Input,
  Label,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@memohai/ui'
import { LoaderCircle, Trash2 } from 'lucide-vue-next'
import {
  deleteOrchestrationEnvResourcesById,
  getOrchestrationContainerImages,
  getOrchestrationEnvResourcesById,
  patchOrchestrationEnvResourcesById,
} from '@memohai/sdk'
import { resolveApiErrorMessage } from '@/utils/api-error'
import KeyValueEditor from '@/components/key-value-editor/index.vue'
import type { KeyValuePair } from '@/components/key-value-editor/index.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const queryCache = useQueryCache()
const saving = ref(false)
const deleting = ref(false)
const deleteOpen = ref(false)

const resourceId = computed(() => String(route.params.id ?? ''))

const { data: resourceData, asyncStatus } = useQuery({
  key: () => ['orchestration-env-resource', resourceId.value],
  query: async () => {
    const { data } = await getOrchestrationEnvResourcesById({
      path: { id: resourceId.value },
      throwOnError: true,
    })
    return data
  },
})

const { data: imageData } = useQuery({
  key: () => ['orchestration-container-images'],
  query: async () => {
    const { data } = await getOrchestrationContainerImages({ throwOnError: true })
    return data
  },
})

const imageOptions = computed(() =>
  (imageData.value?.items ?? []).filter((image) => image.status === 'ready' && image.image_ref),
)
const resource = computed(() => resourceData.value)

const form = reactive({
  name: '',
  status: 'active',
  imageId: '',
})
const envPairs = ref<KeyValuePair[]>([])

const selectedImage = computed(() =>
  imageOptions.value.find((image) => image.id === form.imageId)?.image_ref ?? '',
)

const canSave = computed(() => {
  if (!form.name.trim()) return false
  if (resource.value?.kind !== 'container') return true
  return selectedImage.value.length > 0
})

watch([resource, imageOptions], ([res, images]) => {
  if (!res) return
  form.name = res.name ?? ''
  form.status = res.status ?? 'active'
  const image = typeof res.config?.image === 'string' ? res.config.image : ''
  form.imageId = images.find((item) => item.image_ref === image)?.id ?? images[0]?.id ?? ''
  envPairs.value = envListToPairs(Array.isArray(res.config?.env) ? res.config.env : [])
}, { immediate: true })

async function handleSave() {
  if (!resource.value || !canSave.value || saving.value) return
  saving.value = true
  try {
    await patchOrchestrationEnvResourcesById({
      path: { id: resourceId.value },
      body: {
        name: form.name.trim(),
        capacity: resource.value.capacity || 1,
        status: form.status,
        config: buildConfig(),
        metadata: resource.value.metadata ?? {},
      },
      throwOnError: true,
    })
    toast.success(t('orchestration.envResourceUpdated'))
    queryCache.invalidateQueries({ key: ['orchestration-env-resources'] })
    queryCache.invalidateQueries({ key: ['orchestration-env-resource', resourceId.value] })
    await router.push({ name: 'orchestration-env-resources' })
  }
  catch (error) {
    toast.error(resolveApiErrorMessage(error, t('orchestration.envResourceUpdateFailed')))
  }
  finally {
    saving.value = false
  }
}

async function handleDelete() {
  if (deleting.value) return
  deleting.value = true
  try {
    await deleteOrchestrationEnvResourcesById({
      path: { id: resourceId.value },
      throwOnError: true,
    })
    toast.success(t('orchestration.envResourceDeleted'))
    queryCache.invalidateQueries({ key: ['orchestration-env-resources'] })
    queryCache.invalidateQueries({ key: ['orchestration-env-resource', resourceId.value] })
    await router.push({ name: 'orchestration-env-resources' })
  }
  catch (error) {
    toast.error(resolveApiErrorMessage(error, t('orchestration.envResourceDeleteFailed')))
  }
  finally {
    deleting.value = false
    deleteOpen.value = false
  }
}

function buildConfig() {
  if (resource.value?.kind !== 'container') return resource.value?.config ?? {}
  return {
    image: selectedImage.value,
    env: envPairsToList(envPairs.value),
  }
}

function envListToPairs(value: unknown[]) {
  return value
    .filter((item): item is string => typeof item === 'string')
    .map((item) => {
      const index = item.indexOf('=')
      if (index < 0) return { key: item, value: '' }
      return {
        key: item.slice(0, index),
        value: item.slice(index + 1),
      }
    })
}

function envPairsToList(pairs: KeyValuePair[]) {
  return pairs
    .map((pair) => {
      const key = pair.key.trim()
      if (!key) return ''
      return `${key}=${pair.value}`
    })
    .filter(Boolean)
}
</script>

<template>
  <section class="mx-auto max-w-2xl p-4">
    <div class="mb-6 flex items-center justify-between gap-3">
      <h2 class="text-lg font-semibold">
        {{ $t('orchestration.editEnv') }}
      </h2>
      <Button
        v-if="resource"
        type="button"
        variant="ghost"
        class="text-destructive hover:text-destructive"
        @click="deleteOpen = true"
      >
        <Trash2 class="mr-2 size-4" />
        {{ $t('common.delete') }}
      </Button>
    </div>

    <div
      v-if="asyncStatus === 'loading'"
      class="flex items-center justify-center rounded-xl border border-dashed border-border/70 py-20 text-sm text-muted-foreground"
    >
      <LoaderCircle class="mr-2 size-4 animate-spin" />
      {{ $t('orchestration.loadingEnvResources') }}
    </div>

    <form
      v-else-if="resource"
      @submit.prevent="handleSave"
    >
      <div class="flex flex-col gap-4">
        <div>
          <Label class="mb-2">
            {{ $t('orchestration.envResourceName') }}
            <span class="text-destructive">*</span>
          </Label>
          <Input
            v-model="form.name"
            type="text"
          />
        </div>

        <div>
          <Label class="mb-2">
            {{ $t('orchestration.envResourceStatus') }}
          </Label>
          <Select v-model="form.status">
            <SelectTrigger class="w-full">
              <SelectValue :placeholder="$t('orchestration.envResourceStatus')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="active">
                {{ $t('orchestration.statusActive') }}
              </SelectItem>
              <SelectItem value="disabled">
                {{ $t('orchestration.statusDisabled') }}
              </SelectItem>
              <SelectItem value="archived">
                {{ $t('orchestration.statusArchived') }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <template v-if="resource.kind === 'container'">
          <div>
            <Label class="mb-2">
              {{ $t('orchestration.envResourceImage') }}
              <span class="text-destructive">*</span>
            </Label>
            <Select v-model="form.imageId">
              <SelectTrigger class="w-full">
                <SelectValue :placeholder="$t('orchestration.envResourceImage')" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="image in imageOptions"
                  :key="image.id"
                  :value="image.id"
                >
                  {{ image.name }} · {{ image.image_ref }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div>
            <Label class="mb-2">
              {{ $t('orchestration.envResourceEnv') }}
            </Label>
            <KeyValueEditor
              v-model="envPairs"
              :key-placeholder="$t('orchestration.envVariableName')"
              :value-placeholder="$t('orchestration.envVariableValue')"
            />
          </div>
        </template>
      </div>

      <div class="mt-6 flex justify-end gap-3 pb-4">
        <Button
          type="button"
          variant="outline"
          @click="router.push({ name: 'orchestration-env-resources' })"
        >
          {{ $t('common.cancel') }}
        </Button>
        <Button
          type="submit"
          :disabled="!canSave || saving"
        >
          <LoaderCircle
            v-if="saving"
            class="mr-2 size-4 animate-spin"
          />
          {{ $t('common.save') }}
        </Button>
      </div>
    </form>

    <Dialog v-model:open="deleteOpen">
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{{ $t('orchestration.deleteEnv') }}</DialogTitle>
        </DialogHeader>
        <p class="text-sm text-muted-foreground">
          {{ $t('orchestration.deleteEnvConfirm') }}
        </p>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            @click="deleteOpen = false"
          >
            {{ $t('common.cancel') }}
          </Button>
          <Button
            type="button"
            variant="destructive"
            :disabled="deleting"
            @click="handleDelete"
          >
            <LoaderCircle
              v-if="deleting"
              class="mr-2 size-4 animate-spin"
            />
            {{ $t('common.delete') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </section>
</template>
