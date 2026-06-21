import api, { getAccessToken, apiBase } from './client'
import type { PositioningResult } from './positioning'

// ── JD-aware coach session ─────────────────────────────────────────────────

export interface JDSessionRequest {
  job_title: string
  company?: string
  location?: string
  jd_text?: string
  overall_match?: number
  tier_fit?: string
  company_bar?: string
  summary?: string
  skill_gaps?: string[]
  action_plan?: string[]
  interview_focus?: string[]
  time_to_ready?: string
}

export interface JDSessionResponse {
  session_id: string
  expires_at: string
  mode: 'jd_aware'
}

export function createJDSession(req: JDSessionRequest) {
  return api.post<JDSessionResponse>('/coach/jd-sessions', req)
}

export function buildJDSessionRequest(
  jobTitle: string,
  company: string,
  location: string,
  jdText: string,
  result: PositioningResult
): JDSessionRequest {
  return {
    job_title: jobTitle,
    company,
    location,
    jd_text: jdText,
    overall_match: result.overall_match,
    tier_fit: result.tier_fit,
    company_bar: result.company_bar,
    summary: result.summary,
    skill_gaps: result.skill_gaps.map(s => s.skill),
    action_plan: result.action_plan.map(a => a.title),
    interview_focus: result.interview_focus,
    time_to_ready: result.time_to_ready,
  }
}

// SSE stream URL for JD sessions. Must be an absolute URL so EventSource works
// across the Vercel (frontend) → Render (backend) cross-origin deployment.
// Falls back to window.location.origin for local dev (picked up by Vite proxy).
export function jdSessionStreamURL(sessionId: string, message: string): string {
  const token = getAccessToken()
  const base = apiBase || (typeof window !== 'undefined' ? window.location.origin : '')
  return `${base}/api/v1/coach/jd-sessions/${sessionId}/stream?message=${encodeURIComponent(message)}&token=${token}`
}

// ── Prep plan ─────────────────────────────────────────────────────────────

export interface PrepDay {
  day: number
  label: string
  topics: string[]
  task: string
  resource?: string
  duration: string
}

export interface PrepWeek {
  week: number
  title: string
  focus: string
  days: PrepDay[]
  milestone: string
}

export interface PrepPlanResponse {
  job_title: string
  company: string
  total_weeks: number
  time_to_ready: string
  overall_match: number
  weeks: PrepWeek[]
  final_tip: string
  generated_at: string
}

export interface PrepPlanRequest {
  job_title: string
  company?: string
  overall_match?: number
  tier_fit?: string
  company_bar?: string
  time_to_ready?: string
  skill_gaps?: string[]
  action_plan?: string[]
  interview_focus?: string[]
  yoe?: number
}

export function generatePrepPlan(req: PrepPlanRequest) {
  return api.post<PrepPlanResponse>('/jobs/prep-plan', req)
}
