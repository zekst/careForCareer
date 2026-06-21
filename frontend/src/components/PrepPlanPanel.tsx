import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import { generatePrepPlan, createJDSession, buildJDSessionRequest, jdSessionStreamURL, type PrepPlanResponse, type PrepWeek } from '../api/prep'
import type { PositioningResult } from '../api/positioning'
import type { Job } from '../api/jobs'

interface Props {
  job: Job
  result: PositioningResult
  yoe?: number
}

interface Message { role: 'user' | 'assistant'; content: string }

export default function PrepPlanPanel({ job, result, yoe = 0 }: Props) {
  // ── Prep plan state ─────────────────────────────────────────────────────
  const [plan, setPlan] = useState<PrepPlanResponse | null>(null)
  const [loadingPlan, setLoadingPlan] = useState(false)
  const [planError, setPlanError] = useState('')
  const [expandedWeek, setExpandedWeek] = useState<number | null>(1)

  // ── JD-aware coach state ────────────────────────────────────────────────
  const [sessionId, setSessionId] = useState('')
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [streaming, setStreaming] = useState(false)
  const [sessionLoading, setSessionLoading] = useState(false)
  const [coachError, setCoachError] = useState('')
  const [coachTab, setCoachTab] = useState<'plan' | 'chat'>('plan')

  const fetchPlan = async () => {
    setLoadingPlan(true)
    setPlanError('')
    try {
      const res = await generatePrepPlan({
        job_title: job.title,
        company: job.company,
        overall_match: result.overall_match,
        tier_fit: result.tier_fit,
        company_bar: result.company_bar,
        time_to_ready: result.time_to_ready,
        skill_gaps: (result.skill_gaps ?? []).map(s => s.skill),
        action_plan: (result.action_plan ?? []).map(a => a.title),
        interview_focus: result.interview_focus,
        yoe,
      })
      setPlan(res.data)
      setExpandedWeek(1)
    } catch (err: any) {
      setPlanError(err.response?.data?.error?.message ?? 'Failed to generate plan')
    } finally {
      setLoadingPlan(false)
    }
  }

  const startCoach = async () => {
    if (sessionId || sessionLoading) return
    setSessionLoading(true)
    setCoachError('')
    try {
      const req = buildJDSessionRequest(job.title, job.company, job.location, job.description, result)
      const res = await createJDSession(req)
      setSessionId(res.data.session_id)
      sendMessage('Hi! Give me a quick summary of what I need to focus on to get this role.', res.data.session_id)
    } catch {
      setCoachError('Could not connect to coach. Check your connection and try again.')
    } finally {
      setSessionLoading(false)
    }
  }

  const sendMessage = async (text?: string, sid?: string) => {
    const msg = text ?? input.trim()
    const useSid = sid ?? sessionId
    if (!msg || streaming || !useSid) return
    if (!text) setInput('')
    setMessages(prev => [...prev, { role: 'user', content: msg }])
    setStreaming(true)
    setCoachError('')

    try {
      const url = jdSessionStreamURL(useSid, msg)
      const evtSource = new EventSource(url)
      let buffer = ''
      setMessages(prev => [...prev, { role: 'assistant', content: '' }])

      evtSource.addEventListener('delta', (e) => {
        try {
          const parsed = JSON.parse(e.data)
          buffer += parsed.delta ?? e.data
        } catch {
          buffer += e.data
        }
        setMessages(prev => {
          const updated = [...prev]
          updated[updated.length - 1] = { role: 'assistant', content: buffer }
          return updated
        })
      })

      const cleanup = () => { evtSource.close(); setStreaming(false) }
      evtSource.addEventListener('done', cleanup)
      evtSource.onerror = cleanup
    } catch {
      setStreaming(false)
      setCoachError('Coach unavailable. Try again.')
    }
  }

  return (
    <div className="space-y-4">
      {/* Tab bar */}
      <div className="flex gap-1 bg-gray-800/60 rounded-xl p-1 border border-gray-700">
        <button onClick={() => setCoachTab('plan')}
          className={`flex-1 py-2 rounded-lg text-sm font-medium transition-colors
            ${coachTab === 'plan' ? 'bg-indigo-600 text-white' : 'text-gray-400 hover:text-gray-200'}`}>
          📅 Study Plan
        </button>
        <button
          onClick={() => { setCoachTab('chat'); if (!sessionId) startCoach() }}
          className={`flex-1 py-2 rounded-lg text-sm font-medium transition-colors
            ${coachTab === 'chat' ? 'bg-indigo-600 text-white' : 'text-gray-400 hover:text-gray-200'}`}>
          🤖 Role-Specific Coach
        </button>
      </div>

      {/* ── Study Plan Tab ──────────────────────────────────────────────── */}
      {coachTab === 'plan' && (
        <div className="space-y-4">
          {!plan && (
            <div className="bg-gray-800 rounded-xl p-5 border border-gray-700 space-y-3">
              <div>
                <p className="text-sm font-medium text-white">Generate your personalised study plan</p>
                <p className="text-xs text-gray-400 mt-1">
                  A week-by-week prep schedule calibrated to your gaps for{' '}
                  <span className="text-indigo-300">{job.title}</span>{job.company ? ` at ${job.company}` : ''}.
                  Estimated time: <span className="text-yellow-300">{result.time_to_ready}</span>.
                </p>
              </div>
              {planError && <p className="text-xs text-red-400">{planError}</p>}
              <button onClick={fetchPlan} disabled={loadingPlan}
                className="w-full bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white font-medium rounded-lg py-2.5 text-sm transition-colors">
                {loadingPlan ? (
                  <span className="flex items-center justify-center gap-2">
                    <span className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                    Building your plan…
                  </span>
                ) : 'Generate study plan →'}
              </button>
            </div>
          )}

          {plan && (
            <div className="space-y-3">
              {/* Plan header */}
              <div className="bg-gray-800 rounded-xl p-4 border border-gray-700">
                <div className="flex items-center justify-between">
                  <div>
                    <p className="text-white font-semibold text-sm">{plan.job_title}{plan.company ? ` · ${plan.company}` : ''}</p>
                    <p className="text-gray-400 text-xs mt-0.5">{plan.total_weeks}-week plan · {plan.time_to_ready}</p>
                  </div>
                  <span className={`text-sm font-bold ${plan.overall_match >= 75 ? 'text-emerald-400' : plan.overall_match >= 50 ? 'text-yellow-400' : 'text-red-400'}`}>
                    {plan.overall_match}% match
                  </span>
                </div>
              </div>

              {/* Weeks */}
              {plan.weeks.map(week => (
                <WeekCard
                  key={week.week}
                  week={week}
                  expanded={expandedWeek === week.week}
                  onToggle={() => setExpandedWeek(expandedWeek === week.week ? null : week.week)}
                />
              ))}

              {/* Final tip */}
              {plan.final_tip && (
                <div className="bg-amber-900/30 border border-amber-800/50 rounded-xl p-4">
                  <p className="text-xs text-amber-400 font-semibold uppercase tracking-wider mb-1">💡 Final tip</p>
                  <p className="text-amber-200 text-sm">{plan.final_tip}</p>
                </div>
              )}

              <button onClick={() => setPlan(null)}
                className="text-xs text-gray-600 hover:text-gray-400 transition-colors">
                ↩ Regenerate plan
              </button>
            </div>
          )}
        </div>
      )}

      {/* ── JD-Aware Coach Tab ───────────────────────────────────────────── */}
      {coachTab === 'chat' && (
        <div className="bg-gray-800 rounded-xl border border-gray-700 flex flex-col h-[500px]">
          <div className="px-4 py-3 border-b border-gray-700">
            <p className="text-sm font-semibold text-white">
              Coach for: <span className="text-indigo-300">{job.title}</span>
              {job.company && <span className="text-gray-400"> at {job.company}</span>}
            </p>
            <p className="text-xs text-gray-500 mt-0.5">
              Every answer is grounded in your positioning for this specific role.
            </p>
          </div>

          {/* Messages */}
          <div className="flex-1 overflow-y-auto px-4 py-3 space-y-3">
            {messages.length === 0 && !streaming && (
              <div className="text-center text-gray-600 text-sm py-8">
                <p className="text-2xl mb-2">🎯</p>
                {sessionLoading
                  ? <><p className="text-gray-400">Connecting to coach…</p>
                      <span className="inline-block w-4 h-4 border-2 border-gray-500/30 border-t-gray-400 rounded-full animate-spin mt-2" /></>
                  : <p>Starting your role-specific prep coach…</p>}
              </div>
            )}
            {messages.map((m, i) => (
              <div key={i} className={`flex ${m.role === 'user' ? 'justify-end' : 'justify-start'}`}>
                <div className={`max-w-[85%] rounded-2xl px-3 py-2.5 text-sm leading-relaxed
                  ${m.role === 'user'
                    ? 'bg-indigo-600 text-white rounded-br-sm'
                    : 'bg-gray-700 text-gray-200 rounded-bl-sm prose prose-invert prose-sm max-w-none'}`}>
                  {m.role === 'assistant'
                    ? <ReactMarkdown>{m.content}</ReactMarkdown>
                    : m.content}
                  {streaming && i === messages.length - 1 && m.role === 'assistant' && (
                    <span className="inline-block w-1.5 h-3.5 bg-gray-400 ml-0.5 animate-pulse" />
                  )}
                </div>
              </div>
            ))}
            {coachError && (
              <div className="text-center space-y-2">
                <p className="text-xs text-red-400">{coachError}</p>
                {!sessionId && (
                  <button onClick={startCoach} disabled={sessionLoading}
                    className="text-xs text-indigo-400 hover:text-indigo-300 border border-indigo-800 px-3 py-1 rounded-lg transition-colors">
                    Retry connection
                  </button>
                )}
              </div>
            )}
          </div>

          {/* Quick prompts */}
          {messages.length <= 2 && !streaming && (
            <div className="px-4 pb-2 flex gap-2 flex-wrap">
              {[
                'What should I study this week?',
                'Give me a mock system design question for this role',
                'What DSA topics will they test?',
              ].map(q => (
                <button key={q} onClick={() => sendMessage(q)}
                  className="text-xs bg-gray-700 hover:bg-gray-600 text-gray-300 px-3 py-1.5 rounded-lg transition-colors">
                  {q}
                </button>
              ))}
            </div>
          )}

          {/* Input */}
          <div className="px-3 py-3 border-t border-gray-700 flex gap-2">
            <input
              value={input}
              onChange={e => setInput(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && !e.shiftKey && sendMessage()}
              placeholder="Ask about this specific role…"
              maxLength={2000}
              disabled={streaming || !sessionId || sessionLoading}
              className="flex-1 bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white placeholder-gray-500 text-sm focus:outline-none focus:border-indigo-500 disabled:opacity-50"
            />
            <button onClick={() => sendMessage()} disabled={!input.trim() || streaming || !sessionId || sessionLoading}
              className="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-40 text-white px-3 py-2 rounded-lg text-sm transition-colors">
              →
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

// ── Week card sub-component ───────────────────────────────────────────────────

function WeekCard({ week, expanded, onToggle }: { week: PrepWeek; expanded: boolean; onToggle: () => void }) {
  const weekColors = [
    'border-blue-800/60 bg-blue-900/10',
    'border-purple-800/60 bg-purple-900/10',
    'border-emerald-800/60 bg-emerald-900/10',
    'border-orange-800/60 bg-orange-900/10',
    'border-red-800/60 bg-red-900/10',
  ]
  const color = weekColors[(week.week - 1) % weekColors.length]

  return (
    <div className={`rounded-xl border overflow-hidden ${color}`}>
      <button onClick={onToggle} className="w-full px-4 py-3 flex items-center justify-between text-left">
        <div>
          <div className="flex items-center gap-2">
            <span className="text-xs font-bold text-gray-500 uppercase tracking-wider">Week {week.week}</span>
            <span className="text-sm font-semibold text-white">{week.title}</span>
          </div>
          <p className="text-xs text-gray-400 mt-0.5">{week.focus}</p>
        </div>
        <span className="text-gray-500 text-sm ml-3">{expanded ? '▲' : '▼'}</span>
      </button>

      {expanded && (
        <div className="px-4 pb-4 space-y-3 border-t border-gray-700/50 pt-3">
          {week.days.map(day => (
            <div key={day.day} className="flex gap-3">
              <div className="shrink-0 w-10 text-center">
                <span className="text-xs font-bold text-gray-500">{day.label}</span>
              </div>
              <div className="flex-1 bg-gray-800/60 rounded-lg px-3 py-2">
                <div className="flex items-start justify-between gap-2">
                  <p className="text-white text-xs font-medium leading-snug">{day.task}</p>
                  <span className="text-gray-600 text-xs shrink-0">{day.duration}</span>
                </div>
                {day.topics.length > 0 && (
                  <div className="flex flex-wrap gap-1 mt-1.5">
                    {day.topics.map(t => (
                      <span key={t} className="text-xs bg-gray-700 text-gray-400 px-1.5 py-0.5 rounded">{t}</span>
                    ))}
                  </div>
                )}
                {day.resource && (
                  <p className="text-indigo-400 text-xs mt-1">📚 {day.resource}</p>
                )}
              </div>
            </div>
          ))}

          <div className="flex items-start gap-2 bg-gray-700/40 rounded-lg px-3 py-2 mt-2">
            <span className="text-xs font-bold text-emerald-400 shrink-0">✓ Goal:</span>
            <p className="text-gray-300 text-xs">{week.milestone}</p>
          </div>
        </div>
      )}
    </div>
  )
}
