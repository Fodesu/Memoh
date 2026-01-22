<template>
  <section>
    <CreateMCP />
    <DataTable
      :columns="columns"
      :data="[]"
    />
  </section>
</template>

<script setup lang="ts">
import { useQuery } from '@pinia/colada'
import request from '@/utils/request'
import { watch, h, provide,ref } from 'vue'
import DataTable from '@/components/DataTable/index.vue'
import CreateMCP from '@/components/CreateMCP/index.vue'

const open=ref(false)
provide('open',open)

const columns = [
  {
    accessorKey: 'modelId',
    header: () => h('div', { class: 'text-left py-4' }, 'Name'),
    cell({ row }) {
      return h('div', { class: 'text-left py-4' }, row.getValue('modelId'))
    }
  },
  {
    accessorKey: 'baseUrl',
    header: () => h('div', { class: 'text-left' }, 'Base Url'),
  },
  {
    accessorKey: 'apiKey',
    header: () => h('div', { class: 'text-left' }, 'Api Key'),
  },
  {
    accessorKey: 'clientType',
    header: () => h('div', { class: 'text-left' }, 'Client Type'),
  },
  {
    accessorKey: 'name',
    header: () => h('div', { class: 'text-left' }, 'Name'),
  },
  {
    accessorKey: 'type',
    header: () => h('div', { class: 'text-left' }, 'Type'),
  },
  {
    accessorKey: 'control',
    header: () => h('div', { class: 'text-center' }, '操作'),

  }
]

const { data: mcpData } = useQuery({
  key: ['mcp'],
  query: () => request({
    url: '/mcp/'
  })
})

watch(mcpData, () => {
  console.log(mcpData.value?.data)
})
</script>