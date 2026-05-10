<template>
  <section class="mx-auto max-w-2xl p-4">
    <h2 class="mb-6 text-lg font-semibold">
      {{ $t('orchestration.addEnv') }}
    </h2>

    <form @submit.prevent="handleSubmit">
      <div class="flex flex-col gap-4">
        <div>
          <Label class="mb-2">
            {{ $t('orchestration.envResourceName') }}
            <span class="text-destructive">*</span>
          </Label>
          <Input
            v-model="form.name"
            type="text"
            placeholder="shared-shell"
          />
        </div>

        <div>
          <Label class="mb-2">
            {{ $t('orchestration.envResourceType') }}
            <span class="text-destructive">*</span>
          </Label>
          <Select v-model="form.type">
            <SelectTrigger class="w-full">
              <SelectValue :placeholder="$t('orchestration.envResourceType')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="container">
                {{ $t('orchestration.envKindContainer') }}
              </SelectItem>
              <SelectItem value="browser">
                {{ $t('orchestration.envKindBrowser') }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <template v-if="form.type === 'container'">
        <div class="mt-4 flex flex-col gap-4">
          <div>
            <Label class="mb-2">
              {{ $t('orchestration.envResourceImage') }}
              <span class="text-destructive">*</span>
            </Label>
            <Select v-model="form.imageOption">
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
        </div>

        <div class="mt-4">
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
          :disabled="!canSubmit || submitLoading"
        >
          <LoaderCircle
            v-if="submitLoading"
            class="mr-2 size-4 animate-spin"
          />
          {{ $t('orchestration.addEnv') }}
        </Button>
      </div>
    </form>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { toast } from 'vue-sonner'
import { useQuery, useQueryCache } from '@pinia/colada'
import {
  Button,
  Input,
  Label,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@memohai/ui'
import { LoaderCircle } from 'lucide-vue-next'
import {
  getOrchestrationContainerImages,
  postOrchestrationEnvResources,
} from '@memohai/sdk'
import { resolveApiErrorMessage } from '@/utils/api-error'
import KeyValueEditor from '@/components/key-value-editor/index.vue'
import type { KeyValuePair } from '@/components/key-value-editor/index.vue'

const { t } = useI18n()
const router = useRouter()
const queryCache = useQueryCache()
const submitLoading = ref(false)

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

const form = reactive({
  name: 'shared-shell',
  type: 'container',
  imageOption: '',
})
const envPairs = ref<KeyValuePair[]>([])

const selectedImage = computed(() =>
  imageOptions.value.find((image) => image.id === form.imageOption)?.image_ref ?? '',
)

watch(imageOptions, (images) => {
  if (!form.imageOption && images[0]?.id) {
    form.imageOption = images[0].id
  }
}, { immediate: true })

const canSubmit = computed(() => {
  if (!form.name.trim()) return false
  if (form.type !== 'container') return true
  return selectedImage.value.length > 0
})

async function handleSubmit() {
  if (!canSubmit.value || submitLoading.value) return
  submitLoading.value = true
  try {
    await postOrchestrationEnvResources({
      body: {
        name: form.name.trim(),
        kind: form.type,
        status: 'active',
        capacity: 1,
        config: buildConfig(),
        metadata: {
          source: 'web',
        },
      },
      throwOnError: true,
    })
    toast.success(t('orchestration.envResourceCreated'))
    queryCache.invalidateQueries({ key: ['orchestration-env-resources'] })
    await router.push({ name: 'orchestration-env-resources' })
  }
  catch (error) {
    toast.error(resolveApiErrorMessage(error, t('orchestration.envResourceCreateFailed')))
  }
  finally {
    submitLoading.value = false
  }
}

function buildConfig() {
  if (form.type !== 'container') return {}
  return {
    image: selectedImage.value,
    env: envPairsToList(envPairs.value),
  }
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
