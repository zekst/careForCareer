import api from './client'

export interface AuthResponse {
  access_token: string
  refresh_token: string
  expires_in: number
  user_id?: string
}

export const register = (email: string, password: string) =>
  api.post<AuthResponse>('/auth/register', { email, password })

export const login = (email: string, password: string) =>
  api.post<AuthResponse>('/auth/login', { email, password })

export const logout = (refresh_token: string) =>
  api.post('/auth/logout', { refresh_token })

export const forgotPassword = (email: string) =>
  api.post<{ message: string }>('/auth/forgot-password', { email })

export const resetPassword = (token: string, password: string) =>
  api.post<{ message: string }>('/auth/reset-password', { token, password })
