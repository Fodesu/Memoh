<template>
  <section>
    <MainLayout>
      <template #sidebar>
        <OrchestrationSidebar />
      </template>
      <template #main>
        <SidebarInset class="flex flex-col overflow-hidden">
          <section class="min-h-0 flex-1 overflow-hidden">
            <router-view v-slot="{ Component }">
              <KeepAlive>
                <component :is="Component" />
              </KeepAlive>
            </router-view>
          </section>
        </SidebarInset>
      </template>
    </MainLayout>
  </section>
</template>

<script setup lang="ts">
import { SidebarInset } from '@memohai/ui'
import MainLayout from '@/layout/main-layout/index.vue'
import OrchestrationSidebar from '@/components/orchestration-sidebar/index.vue'
</script>
