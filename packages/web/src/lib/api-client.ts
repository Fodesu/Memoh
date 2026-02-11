import { client } from '@memoh/sdk/client'
import router from '@/router'

/**
 * Configure the SDK client with base URL, auth interceptor, and 401 handling.
 * Call this once at app startup (main.ts).
 */
export function setupApiClient() {
  // Set base URL to match the Vite proxy
  client.setConfig({ baseUrl: '/api' })

  // Add auth token to every request
  client.interceptors.request.use((request) => {
    const token = localStorage.getItem('token')
    if (token) {
      request.headers.set('Authorization', `Bearer ${token}`)
    }
    return request
  })

  // Handle 401 responses globally
  client.interceptors.response.use((response) => {
    if (response.status === 401) {
      localStorage.removeItem('token')
      router.replace({ name: 'Login' })
    }
    return response
  })
}
