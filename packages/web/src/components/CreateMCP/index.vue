<template>
  <section class="flex">
    <Dialog v-model:open="open">
      <DialogTrigger as-child>
        <Button
          variant="default"
          class="ml-auto my-4"
        >
          添加MCP
        </Button>
      </DialogTrigger>
      <DialogContent class="sm:max-w-106.25">
        <form @submit="createMCP">
          <DialogHeader>
            <DialogTitle>添加MCP</DialogTitle>
            <DialogDescription class="mb-4">
              添加MCP完成操作
            </DialogDescription>
          </DialogHeader>
          <div>
            <FormField
              v-slot="{ componentField }"
              name="name"
            >
              <FormItem>
                <FormLabel class="mb-2">
                  Name
                </FormLabel>
                <FormControl>
                  <Input
                    type="text"
                    placeholder="请输入Name"
                    v-bind="componentField"
                    autocomplete="name"
                  />
                </FormControl>
                <blockquote class="h-5">
                  <FormMessage />
                </blockquote>
              </FormItem>
            </FormField>
            <FormField
              v-slot="{ componentField }"
              name="config.type"
            >
              <FormItem>
                <FormLabel class="mb-2">
                  Type
                </FormLabel>
                <FormControl>
                  <Select v-bind="componentField">
                    <SelectTrigger class="w-full">
                      <SelectValue placeholder="请选择 Type" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem value="stdio">
                          Stdio
                        </SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </FormControl>
                <blockquote class="h-5">
                  <FormMessage />
                </blockquote>
              </FormItem>
            </FormField>
            <FormField
              v-slot="{ componentField }"
              name="config.cwd"
            >
              <FormItem>
                <FormLabel class="mb-2">
                  Cwd
                </FormLabel>
                <FormControl>
                  <Input
                    type="text"
                    placeholder="请输入cwd"
                    v-bind="componentField"
                    autocomplete="cwd"
                  />
                </FormControl>
                <blockquote class="h-5">
                  <FormMessage />
                </blockquote>
              </FormItem>
            </FormField>
            <FormField
              v-slot="{ componentField }"
              name="config.command"
            >
              <FormItem>
                <FormLabel class="mb-2">
                  Command
                </FormLabel>
                <FormControl>
                  <Input
                    placeholder="请输入Command"
                    v-bind="componentField"
                  />
                </FormControl>
                <blockquote class="h-5">
                  <FormMessage />
                </blockquote>
              </FormItem>
            </FormField>
            <FormField
              v-slot="{ componentField }"
              name="config.args"
            >
              <FormItem>
                <FormLabel class="mb-2">
                  Arguments
                </FormLabel>
                <FormControl>
                  <TagsInput
                    v-model="componentField.modelValue"
                    :add-on-blur="true"
                    :duplicate="true"
                    @update:model-value="componentField['onUpdate:modelValue']"
                  >
                    <TagsInputItem
                      v-for="item in componentField.modelValue"
                      :key="item"
                      :value="item"
                    >
                      <TagsInputItemText />
                      <TagsInputItemDelete />
                    </TagsInputItem>
                    <TagsInputInput
                      placeholder="请输入Arguments"
                      class="w-full py-1"
                    />
                  </TagsInput>
                </FormControl>
                <blockquote class="h-5">
                  <FormMessage />
                </blockquote>
              </FormItem>
            </FormField>
            <FormField
              v-slot="{ componentField }"
              name="config.env"
            >
              <FormItem>
                <FormLabel class="mb-2">
                  Env
                </FormLabel>
                <FormControl>
                  <TagsInput
                    :add-on-blur="true"
                    :model-value="envList"
                    :convert-value="tagStr => {
                      if (/^\w+\:\w+$/.test(tagStr)) {
                        return tagStr
                      }
                      return ''
                    }"
                    @update:model-value="(env) => {
                      envList = env.filter(Boolean) as string[]
                      const curEnvObject: { [key in string]: string } = {}
                      envList.forEach(envItem => {
                        const [key, value] = envItem.split(`:`);
                        if (key && value) {
                          curEnvObject[key] = value
                        }
                      })
                      componentField['onUpdate:modelValue']?.(curEnvObject)
                    }"
                  >
                    <TagsInputItem
                      v-for="(value, index) in envList"
                      :key="index"
                      :value="value as string"
                    >
                      <TagsInputItemText />
                      <TagsInputItemDelete />
                    </TagsInputItem>
                    <TagsInputInput
                      placeholder="请输入Env"
                      class="w-full py-1"
                    />
                  </TagsInput>
                </FormControl>
                <blockquote class="h-5">
                  <FormMessage />
                </blockquote>
              </FormItem>
            </FormField>
            <FormField
              v-slot="{ componentField }"
              name="active"
            >
              <FormItem>
                <FormControl>
                  <section class="flex gap-4">
                    <Label
                   
                      for="airplane-mode"
                    >开启</Label>
                    <Switch
                      id="airplane-mode"
                      :model-value="componentField.modelValue"
                      @update:model-value="componentField['onUpdate:modelValue']"
                    />
                  </section>
                </FormControl>
                <blockquote class="h-5">
                  <FormMessage />
                </blockquote>
              </FormItem>
            </FormField>
          </div>
          <DialogFooter class="mt-4">
            <DialogClose as-child>
              <Button variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit">
              添加Model
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  </section>
</template>
<script setup lang="ts">
import {
  Button,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  Input,
  FormField,
  FormControl,
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
  FormItem,
  FormLabel,
  FormMessage,
  TagsInput,
  TagsInputInput,
  TagsInputItem,
  TagsInputItemDelete,
  TagsInputItemText,
  Switch,
  Label
} from '@memoh/ui'
import z from 'zod'
import { toTypedSchema } from '@vee-validate/zod'
import { useForm } from 'vee-validate'
import { ref,inject } from 'vue'
import { useMutation,useQueryCache } from '@pinia/colada'
import request from '@/utils/request'

const validateSchema = toTypedSchema(z.object({
  name: z.string().min(1),
  config: z.object({
    type: z.string().min(1),
    command: z.string().min(1),
    args: z.array(z.coerce.string().check(z.minLength(1))).min(1),
    env: z.looseObject({}),
    cwd:z.string().min(1)
  }),
  active:z.coerce.boolean()
}))

const envList = ref<string[]>([])
const form = useForm({
  validationSchema: validateSchema
})


const queryCache=useQueryCache()
const { mutate: fetchMCP } = useMutation({
  mutation: (data: Parameters<(Parameters<typeof form.handleSubmit>)[0]>[0]) => request({
    url: '/mcp/',
    method: 'post',
    data
  }),
  onSettled:()=>queryCache.invalidateQueries({key:['mcp']})
})

const open=inject('open',ref(false))
const createMCP = form.handleSubmit(async (value) => {
  // console.log(value)
  try {
    fetchMCP(value)
    open.value=false
  } catch {
    return
  }

})
</script>