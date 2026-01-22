<template>
  <aside class="[&_[data-state=collapsed]_:is(.title-container,.exist-btn)]:hidden">
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <img
              src="../../../public/logo.png"
              width="80"
              class="m-auto"
              alt="logo.png"
            >
            <h4
              class="scroll-m-20 text-xl font-semibold tracking-tight text-center text-muted-foreground title-container"
            >
              Memoh
            </h4>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>
            对话操作
          </SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <Collapsible
                v-for="sidebarItem in sidebarInfo"
                :key="sidebarItem.title"
                class="group/collapsible"
              >
                <SidebarMenuItem>
                  <CollapsibleTrigger as-child>
                    <SidebarMenuButton
                      :tooltip="sidebarItem.title"
                      @click="router.push({ name: sidebarItem.name })"
                    >
                      <svg-icon
                        type="mdi"
                        :path="sidebarItem.icon"
                      />
                      <span>{{ sidebarItem.title }}</span>
                    </SidebarMenuButton>
                  </CollapsibleTrigger>
                </SidebarMenuItem>
              </Collapsible>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem class="flex justify-center exist-btn">
            <Button
              class="flex-[0.7] mb-10"
              @click="exit"
            >
              退出登录
            </Button>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarRail />
    </Sidebar>
  </aside>
</template>
<script setup lang="ts">
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  CollapsibleTrigger,
  Collapsible,
  Button
} from '@memoh/ui'
import { reactive } from 'vue'
import SvgIcon from '@jamescoyle/vue-icon'
import { mdiRobot, mdiChatOutline, mdiCogBox, mdiListBox, mdiHome, mdiBookArrowDown } from '@mdi/js'
import { useRouter } from 'vue-router'
import {useUserStore} from '@/store/User.ts'


const router = useRouter()

const sidebarInfo = reactive([{
  title: '创建对话',
  name: 'chat',
  icon: mdiChatOutline
}, {
  title: '主页',
  name: 'home',
  icon: mdiHome
},
{
  title: '模型配置',
  name: 'models',
  icon: mdiRobot
}, {
  title: '设置',
  name: 'settings',
  icon: mdiCogBox
}, {
  title: 'MCP',
  name: 'mcp',
  icon: mdiListBox
}, {
  title: '平台',
  name: 'platform',
  icon: mdiBookArrowDown
  }])

  const {exitLogin}=useUserStore()
const exit = () => {
  exitLogin()
  router.replace({name:'Login'})
}
</script>