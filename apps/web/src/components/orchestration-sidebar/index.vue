<template>
  <aside class="relative h-full">
    <header
      v-if="topInset"
      class="fixed left-0 top-0 z-20 h-9 w-(--sidebar-width) bg-sidebar border-r border-sidebar-border [-webkit-app-region:drag]"
    />

    <Sidebar
      :collapsible="topInset ? 'none' : 'icon'"
      :class="topInset ? 'pt-9 h-dvh border-r border-sidebar-border' : ''"
    >
      <SidebarHeader class="p-0 border-0">
        <button
          class="flex h-[53px] w-full items-center gap-2.5 border-b border-border px-3.5 text-foreground transition-colors hover:bg-accent/50 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0"
          @click="router.push(backToChatRoute)"
        >
          <ChevronLeft class="size-3 shrink-0" />
          <span class="text-xs font-semibold group-data-[collapsible=icon]:hidden">
            {{ t('sidebar.orchestration') }}
          </span>
        </button>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup class="px-2 py-2.5">
          <SidebarGroupContent>
            <SidebarMenu class="gap-0.5">
              <SidebarMenuItem
                v-for="item in navItems"
                :key="item.name"
              >
                <SidebarMenuButton
                  :tooltip="item.title"
                  :is-active="isItemActive(item.name)"
                  :aria-current="isItemActive(item.name) ? 'page' : undefined"
                  class="relative h-9 gap-2 before:absolute before:bottom-1.5 before:left-0 before:top-1.5 before:w-0.5 before:rounded-full data-[active=true]:before:bg-foreground/70 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0"
                  @click="router.push({ name: item.name })"
                >
                  <component
                    :is="item.icon"
                    class="ml-1.5 size-3.5 group-data-[collapsible=icon]:ml-0"
                  />
                  <span class="text-xs font-medium group-data-[collapsible=icon]:hidden">{{ item.title }}</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarRail v-if="!topInset" />
    </Sidebar>
  </aside>
</template>

<script setup lang="ts">
import { computed, inject, type Component } from 'vue'
import { storeToRefs } from 'pinia'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ChevronLeft, Container, Server, Workflow } from 'lucide-vue-next'
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from '@memohai/ui'
import { useChatSelectionStore } from '@/store/chat-selection'
import { DesktopShellKey } from '@/lib/desktop-shell'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const topInset = inject(DesktopShellKey, false)
const selectionStore = useChatSelectionStore()
const { currentBotId } = storeToRefs(selectionStore)

const backToChatRoute = computed(() => {
  const botId = (currentBotId.value ?? '').trim()
  if (!botId) return { name: 'home' as const }
  return {
    name: 'chat' as const,
    params: { botId },
  }
})

const navItems: { title: string, name: string, icon: Component }[] = [
  { title: t('orchestration.runs'), name: 'orchestration', icon: Workflow },
  { title: t('orchestration.envResources'), name: 'orchestration-env-resources', icon: Server },
  { title: t('orchestration.images'), name: 'orchestration-images', icon: Container },
]

function isItemActive(name: string) {
  if (name === 'orchestration-env-resources') {
    return String(route.name ?? '').startsWith('orchestration-env-resources')
  }
  if (name === 'orchestration-images') {
    return String(route.name ?? '').startsWith('orchestration-images')
  }
  return route.name === name
}
</script>
