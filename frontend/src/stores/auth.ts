import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { completeTOTPLogin, getCurrentUser, login as loginRequest } from '@/api/auth'
import type { AuthUser } from '@/types'

const TOKEN_KEY = 'asterrouter_admin_token'
const USER_KEY = 'asterrouter_admin_user'

export const useAuthStore = defineStore('auth', () => {
  const token = ref(localStorage.getItem(TOKEN_KEY) || '')
  const user = ref<AuthUser | null>(readStoredUser())
  const loading = ref(false)
  const error = ref('')

  const isAuthenticated = computed(() => Boolean(token.value))

  async function login(username: string, password: string) {
    loading.value = true
    error.value = ''
    try {
      const result = await loginRequest(username, password)
      token.value = result.access_token
      user.value = result.user
      localStorage.setItem(TOKEN_KEY, result.access_token)
      localStorage.setItem(USER_KEY, JSON.stringify(result.user))
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Login failed'
      throw err
    } finally {
      loading.value = false
    }
  }

  async function loadCurrentUser() {
    if (!token.value) return
    try {
      user.value = await getCurrentUser()
      localStorage.setItem(USER_KEY, JSON.stringify(user.value))
    } catch {
      logout()
    }
  }

  async function completeOIDCLogin() {
		token.value = 'oidc-cookie'
		localStorage.setItem(TOKEN_KEY, token.value)
		try {
			user.value = await getCurrentUser()
			localStorage.setItem(USER_KEY, JSON.stringify(user.value))
		} catch (err) {
			logout()
			throw err
		}
	}

	async function completeMFA(challenge: string, code: string) {
		loading.value = true; error.value = ''
		try { const result = await completeTOTPLogin(challenge, code); token.value = result.access_token; user.value = result.user; localStorage.setItem(TOKEN_KEY, result.access_token); localStorage.setItem(USER_KEY, JSON.stringify(result.user)) }
		catch (err) { error.value = err instanceof Error ? err.message : 'MFA failed'; throw err }
		finally { loading.value = false }
	}

  function logout() {
    token.value = ''
    user.value = null
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(USER_KEY)
  }

  return {
    token,
    user,
    loading,
    error,
    isAuthenticated,
    login,
    loadCurrentUser,
		completeOIDCLogin,
		completeMFA,
    logout
  }
})

function readStoredUser(): AuthUser | null {
  const raw = localStorage.getItem(USER_KEY)
  if (!raw) return null
  try {
    return JSON.parse(raw) as AuthUser
  } catch {
    return null
  }
}
