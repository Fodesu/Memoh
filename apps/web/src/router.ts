import {
  createRouter,
  createWebHistory,
  type RouteLocationNormalized,
} from 'vue-router'
import { h } from 'vue'
import { RouterView } from 'vue-router'
import { i18nRef } from './i18n'

const routes = [
  {
    path: '/',
    component: () => import('@/pages/main-section/index.vue'),
    children: [
      {
        name: 'home',
        path: '',
        component: () => import('@/pages/home/index.vue'),
        meta: {
          breadcrumb: i18nRef('sidebar.chat'),
        },
      },
      {
        name: 'chat',
        path: '/chat/:botId?',
        component: () => import('@/pages/home/index.vue'),
        meta: {
          breadcrumb: i18nRef('sidebar.chat'),
        },
      },
    ],
  },
  {
    path: '/orchestration',
    component: () => import('@/pages/orchestration-section/index.vue'),
    redirect: '/orchestration/runs',
    children: [
      {
        name: 'orchestration',
        path: 'runs',
        component: () => import('@/pages/orchestration/index.vue'),
        meta: {
          breadcrumb: i18nRef('sidebar.orchestration'),
        },
      },
      {
        name: 'orchestration-env-resources',
        path: 'env-resources',
        component: () => import('@/pages/orchestration/env-resources.vue'),
        meta: {
          breadcrumb: i18nRef('orchestration.envResources'),
        },
      },
      {
        name: 'orchestration-env-resources-new',
        path: 'env-resources/new',
        component: () => import('@/pages/orchestration/env-resource-new.vue'),
        meta: {
          breadcrumb: i18nRef('orchestration.addEnv'),
        },
      },
      {
        name: 'orchestration-env-resource-detail',
        path: 'env-resources/:id',
        component: () => import('@/pages/orchestration/env-resource-detail.vue'),
        meta: {
          breadcrumb: i18nRef('orchestration.editEnv'),
        },
      },
      {
        name: 'orchestration-images',
        path: 'images',
        component: () => import('@/pages/orchestration/images.vue'),
        meta: {
          breadcrumb: i18nRef('orchestration.images'),
        },
      },
      {
        name: 'orchestration-images-new',
        path: 'images/new',
        component: () => import('@/pages/orchestration/image-new.vue'),
        meta: {
          breadcrumb: i18nRef('orchestration.addImage'),
        },
      },
      {
        name: 'orchestration-image-detail',
        path: 'images/:id',
        component: () => import('@/pages/orchestration/image-detail.vue'),
        meta: {
          breadcrumb: i18nRef('orchestration.imageDetail'),
        },
      },
    ],
  },
  {
    path: '/settings',
    component: () => import('@/pages/settings-section/index.vue'),
    redirect: '/settings/bots',
    children: [
      {
        path: 'bots',
        component: { render: () => h(RouterView) },
        meta: {
          breadcrumb: i18nRef('sidebar.bots'),
        },
        children: [
          {
            name: 'bots',
            path: '',
            component: () => import('@/pages/bots/index.vue'),
          },
          {
            name: 'bot-new',
            path: 'new',
            component: () => import('@/pages/bots/new.vue'),
            meta: {
              breadcrumb: i18nRef('bots.createBot'),
            },
          },
          {
            name: 'bot-detail',
            path: ':botId',
            component: () => import('@/pages/bots/detail.vue'),
            meta: {
              breadcrumb: (route: RouteLocationNormalized) => route.params.botId,
            },
          },
        ],
      },
      {
        name: 'providers',
        path: 'providers',
        component: () => import('@/pages/providers/index.vue'),
        meta: {
          breadcrumb: i18nRef('sidebar.providers'),
        },
      },
      {
        name: 'web-search',
        path: 'web-search',
        component: () => import('@/pages/web-search/index.vue'),
        meta: {
          breadcrumb: i18nRef('sidebar.webSearch'),
        },
      },
      {
        name: 'memory',
        path: 'memory',
        component: () => import('@/pages/memory/index.vue'),
        meta: {
          breadcrumb: i18nRef('sidebar.memory'),
        },
      },
      {
        name: 'speech',
        path: 'speech',
        component: () => import('@/pages/speech/index.vue'),
        meta: {
          breadcrumb: i18nRef('sidebar.speech'),
        },
      },
      {
        name: 'transcription',
        path: 'transcription',
        component: () => import('@/pages/transcription/index.vue'),
        meta: {
          breadcrumb: i18nRef('sidebar.transcription'),
        },
      },
      {
        name: 'email',
        path: 'email',
        component: () => import('@/pages/email/index.vue'),
        meta: {
          breadcrumb: i18nRef('sidebar.email'),
        },
      },
      {
        name: 'usage',
        path: 'usage',
        component: () => import('@/pages/usage/index.vue'),
        meta: {
          breadcrumb: i18nRef('sidebar.usage'),
        },
      },
      {
        name: 'appearance',
        path: 'appearance',
        component: () => import('@/pages/appearance/index.vue'),
        meta: {
          breadcrumb: i18nRef('sidebar.appearance'),
        },
      },
      {
        name: 'profile',
        path: 'profile',
        component: () => import('@/pages/profile/index.vue'),
        meta: {
          breadcrumb: i18nRef('sidebar.settings'),
        },
      },
      {
        name: 'platform',
        path: 'platform',
        component: () => import('@/pages/platform/index.vue'),
        meta: {
          breadcrumb: i18nRef('sidebar.platform'),
        },
      },
      {
        name: 'supermarket',
        path: 'supermarket',
        component: () => import('@/pages/supermarket/index.vue'),
        meta: {
          breadcrumb: i18nRef('sidebar.supermarket'),
        },
      },
      {
        name: 'about',
        path: 'about',
        component: () => import('@/pages/about/index.vue'),
        meta: {
          breadcrumb: i18nRef('sidebar.about'),
        },
      },
    ],
  },
  {
    name: 'Login',
    path: '/login',
    component: () => import('@/pages/login/index.vue'),
  },
  {
    name: 'oauth-mcp-callback',
    path: '/oauth/mcp/callback',
    component: () => import('@/pages/oauth/mcp-callback.vue'),
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

// Handle chunk load errors (e.g. user aborted refresh, network failure, new deployment)
router.onError((error) => {
  const isChunkLoadError =
    error.message.includes('Failed to fetch dynamically imported module') ||
    error.message.includes('Importing a module script failed') ||
    error.message.includes('error loading dynamically imported module')
  if (isChunkLoadError) {
    console.warn('[Router] Chunk load failed, reloading...', error.message)
    window.location.reload()
    return
  }
  throw error
})

router.beforeEach((to) => {
  const token = localStorage.getItem('token')

  if (to.fullPath === '/login') {
    return token ? { path: '/' } : true
  }
  if (to.path.startsWith('/oauth/')) {
    return true
  }
  return token ? true : { name: 'Login' }
})

export default router
