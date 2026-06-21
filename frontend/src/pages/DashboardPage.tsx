import { useState, useEffect, useRef } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import api, { getAccessToken, apiBase } from '../api/client'
import { getProfile, type Candidate } from '../api/candidate'
import { useAuth } from '../context/AuthContext'
import PositioningPanel from '../components/PositioningPanel'
import type { Job } from '../api/jobs'

interface Message { role: 'user' | 'assistant'; content: string }

const TIER_COLORS = ['bg-gray-500', 'bg-blue-500', 'bg-emerald-500', 'bg-purple-500', 'bg-amber-500']

export default function DashboardPage() {
  const [candidate, setCandidate] = useState<Candidate | null>(null)
  const [searchParams] = useSearchParams()
  const [jdText, setJdText] = useState(() => {
    const fromQuery = searchParams.get('jd')
    return fromQuery ? decodeURIComponent(fromQuery) : ''
  })
  const [jdSubmitted, setJdSubmitted] = useState(false)
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [streaming, setStreaming] = useState(false)
  const [sessionId, setSessionId] = useState('')
  const [tab, setTab] = useState<'jd' | 'position' | 'coach'>('jd')
  const chatBottomRef = useRef<HTMLDivElement>(null)
  const { signOut } = useAuth()
  const navigate = useNavigate()

  // Build a synthetic Job object from URL params so PositioningPanel works in dashboard
  const jobFromParams: Job | null = (() => {
    const jd = searchParams.get('jd')
    const title = searchParams.get('job_title')
    const company = searchParams.get('company')
    const location = searchParams.get('location')
    if (!jd) return null
    return {
      id: 'from-search',
      title: title ? decodeURIComponent(title) : 'Target Role',
      company: company ? decodeURIComponent(company) : '',
      location: location ? decodeURIComponent(location) : '',
      apply_url: '',
      description: decodeURIComponent(jd),
      source: 'search',
    }
  })()

  // If JD was pre-filled from job search, go straight to Positioning tab
  useEffect(() => {
    if (searchParams.get('jd')) setTab('position')
  }, [])

  useEffect(() => {
    getProfile()
      .then(r => setCandidate(r.data))
      .catch(() => navigate('/onboarding'))
  }, [])

  useEffect(() => {
    chatBottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  const submitJD = () => {
    if (!jdText.trim()) return
    setJdSubmitted(true)
    setTab('position')
  }

  const sendMessage = async () => {
    if (!input.trim() || streaming) return
    const userMsg = input.trim()
    setInput('')
    setMessages(prev => [...prev, { role: 'user', content: userMsg }])
    setStreaming(true)

    try {
      // Create a JD-aware session on first message (no assessment_id required)
      let sid = sessionId
      if (!sid) {
        const res = await api.post('/coach/jd-sessions', {
          job_title: jobFromParams?.title || 'Target Role',
          company: jobFromParams?.company || '',
          location: jobFromParams?.location || '',
          jd_text: jdText || jobFromParams?.description || '',
        })
        sid = res.data.session_id
        setSessionId(sid)
      }

      // SSE stream — absolute URL required for cross-origin Vercel→Render deployment
      const token = getAccessToken()
      const sseBase = apiBase || window.location.origin
      const evtSource = new EventSource(
        `${sseBase}/api/v1/coach/jd-sessions/${sid}/stream?message=${encodeURIComponent(userMsg)}&token=${token}`
      )

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
    } catch (err: any) {
      setStreaming(false)
      setMessages(prev => [...prev, { role: 'assistant', content: '⚠️ Coach unavailable right now.' }])
    }
  }

  return (
    <div className="min-h-screen bg-gray-950 text-white">
      {/* Header */}
      <header className="border-b border-gray-800 px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <span className="text-xl font-bold text-indigo-400">careForCareer</span>
          {candidate && (
            <span className={`${TIER_COLORS[candidate.tier]} text-white text-xs font-semibold px-2.5 py-1 rounded-full`}>
              {candidate.tier_label}
            </span>
          )}
        </div>
        <div className="flex items-center gap-4">
          {candidate && (
            <span className="text-gray-400 text-sm">{candidate.current_company || 'No company set'}</span>
          )}
          <button onClick={() => { signOut(); navigate('/login') }}
            className="text-gray-500 hover:text-gray-300 text-sm transition-colors">
            Sign out
          </button>
        </div>
      </header>

      <div className="max-w-5xl mx-auto px-4 py-8 grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Left: Profile card */}
        <div className="lg:col-span-1 space-y-4">
          {candidate && (
            <div className="bg-gray-900 rounded-2xl p-6 border border-gray-800">
              <h3 className="text-sm font-semibold text-gray-400 uppercase tracking-wider mb-4">Profile</h3>
              <div className="space-y-3">
                <div>
                  <p className="text-xs text-gray-500">Experience</p>
                  <p className="text-white font-medium">{candidate.years_experience} years</p>
                </div>
                <div>
                  <p className="text-xs text-gray-500">Tier</p>
                  <p className="text-white font-medium">{candidate.tier_label}</p>
                  <p className="text-gray-500 text-xs mt-0.5">{candidate.tier_explanation}</p>
                </div>
                {candidate.current_comp_inr > 0 && (
                  <div>
                    <p className="text-xs text-gray-500">Current CTC</p>
                    <p className="text-white font-medium">₹{(candidate.current_comp_inr / 100000).toFixed(1)}L</p>
                  </div>
                )}
                {candidate.target_comp_inr > 0 && (
                  <div>
                    <p className="text-xs text-gray-500">Target CTC</p>
                    <p className="text-emerald-400 font-medium">₹{(candidate.target_comp_inr / 100000).toFixed(1)}L</p>
                  </div>
                )}
              </div>
              <button onClick={() => navigate('/onboarding')}
                className="mt-4 w-full text-sm text-gray-500 hover:text-gray-300 border border-gray-700 hover:border-gray-600 rounded-lg py-2 transition-colors">
                Edit profile
              </button>
            </div>
          )}

          <div className="bg-gray-900 rounded-2xl p-6 border border-gray-800">
            <h3 className="text-sm font-semibold text-gray-400 uppercase tracking-wider mb-3">Steps</h3>
            <div className="space-y-2">
              {[
                { label: 'Create profile', done: true },
                { label: 'Upload resume', done: true },
                { label: 'Select target role', done: !!jobFromParams || !!jdText },
                { label: 'Check positioning', done: !!jobFromParams || jdSubmitted },
                { label: 'Chat with coach', done: messages.length > 0 },
              ].map(({ label, done }) => (
                <div key={label} className="flex items-center gap-2 text-sm">
                  <span className={done ? 'text-emerald-400' : 'text-gray-600'}>
                    {done ? '✓' : '○'}
                  </span>
                  <span className={done ? 'text-gray-300' : 'text-gray-500'}>{label}</span>
                </div>
              ))}
            </div>
          </div>
        </div>

        {/* Right: JD + Coach */}
        <div className="lg:col-span-2">
          <div className="flex gap-1 mb-4 bg-gray-900 rounded-xl p-1 border border-gray-800">
            {([
              { key: 'jd', label: '📋 Target Job' },
              { key: 'position', label: '📊 Positioning' },
              { key: 'coach', label: '🤖 Coach' },
            ] as const).map(({ key, label }) => (
              <button key={key} onClick={() => setTab(key)}
                className={`flex-1 py-2 rounded-lg text-sm font-medium transition-colors
                  ${tab === key ? 'bg-indigo-600 text-white' : 'text-gray-400 hover:text-gray-200'}`}>
                {label}
              </button>
            ))}
          </div>

          {tab === 'jd' && (
            <div className="bg-gray-900 rounded-2xl p-6 border border-gray-800 space-y-4">
              <div>
                <h2 className="text-lg font-semibold">Target job description</h2>
                <p className="text-gray-400 text-sm mt-1">
                  {jdText && !jdSubmitted
                    ? '✓ Job description loaded from your search — review and submit.'
                    : "Paste a JD or pick a role from job search. We'll measure your readiness."}
                </p>
              </div>

              {jdSubmitted ? (
                <div className="bg-emerald-900/40 border border-emerald-700 rounded-lg px-4 py-3 text-emerald-300 text-sm">
                  ✓ JD submitted — check your{' '}
                  <button onClick={() => setTab('position')} className="underline font-semibold">Positioning</button>{' '}
                  score, then visit{' '}
                  <button onClick={() => setTab('coach')} className="underline font-semibold">Coach</button>{' '}
                  for your prep plan.
                </div>
              ) : (
                <>
                  <textarea
                    value={jdText}
                    onChange={e => setJdText(e.target.value)}
                    rows={12}
                    maxLength={20000}
                    placeholder="Paste the full job description here…"
                    className="w-full bg-gray-800 border border-gray-700 rounded-xl px-4 py-3 text-white placeholder-gray-500 text-sm focus:outline-none focus:border-indigo-500 resize-none"
                  />
                  {jdText.length > 15000 && (
                    <p className="text-xs text-yellow-500">{jdText.length}/20000 characters — getting long, trimming recommended</p>
                  )}
                  <button onClick={submitJD} disabled={!jdText.trim()}
                    className="w-full bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white font-medium rounded-lg py-2.5 transition-colors">
                    Analyse JD →
                  </button>
                </>
              )}
            </div>
          )}

          {tab === 'position' && (
            <div className="bg-gray-900 rounded-2xl p-6 border border-gray-800 space-y-4">
              <div>
                <h2 className="text-lg font-semibold">Your positioning</h2>
                <p className="text-gray-400 text-sm mt-1">
                  How strong are you for this role — and exactly what to do to improve your chances.
                </p>
              </div>

              {jobFromParams ? (
                <PositioningPanel job={jobFromParams} yoe={candidate?.years_experience} />
              ) : jdText ? (
                <PositioningPanel job={{
                  id: 'manual',
                  title: 'Target Role',
                  company: '',
                  location: '',
                  apply_url: '',
                  description: jdText,
                  source: 'manual',
                }} yoe={candidate?.years_experience} />
              ) : (
                <div className="text-center py-10 text-gray-500 space-y-2">
                  <p className="text-3xl">🎯</p>
                  <p className="text-sm">No target role selected yet.</p>
                  <p className="text-xs text-gray-600">Paste a JD in the "Target Job" tab first, then come back here.</p>
                  <button onClick={() => setTab('jd')}
                    className="mt-3 text-indigo-400 hover:text-indigo-300 text-sm transition-colors">
                    Add target JD →
                  </button>
                </div>
              )}
            </div>
          )}

          {tab === 'coach' && (
            <div className="bg-gray-900 rounded-2xl border border-gray-800 flex flex-col h-[600px]">
              <div className="px-6 py-4 border-b border-gray-800">
                <h2 className="font-semibold">AI Coach</h2>
                <p className="text-gray-500 text-xs mt-0.5">Powered by Claude · 20 messages/day</p>
              </div>

              <div className="flex-1 overflow-y-auto px-6 py-4 space-y-4">
                {!jdText && !jobFromParams && messages.length === 0 && (
                  <div className="bg-amber-900/20 border border-amber-800/40 rounded-lg px-4 py-3 text-amber-300 text-sm">
                    💡 For best results, add a <button onClick={() => setTab('jd')} className="underline font-semibold">target job description</button> first — the coach uses it to give role-specific advice.
                  </div>
                )}
                {messages.length === 0 && (
                  <div className="text-center text-gray-600 text-sm mt-8">
                    <p className="text-3xl mb-3">🧭</p>
                    <p>Ask your coach anything about interview prep,</p>
                    <p>your skill gaps, or what to study next.</p>
                  </div>
                )}
                {messages.map((m, i) => (
                  <div key={i} className={`flex ${m.role === 'user' ? 'justify-end' : 'justify-start'}`}>
                    <div className={`max-w-[80%] rounded-2xl px-4 py-3 text-sm
                      ${m.role === 'user'
                        ? 'bg-indigo-600 text-white rounded-br-sm'
                        : 'bg-gray-800 text-gray-200 rounded-bl-sm prose prose-invert prose-sm max-w-none'}`}>
                      {m.role === 'assistant'
                        ? <ReactMarkdown>{m.content}</ReactMarkdown>
                        : m.content}
                      {streaming && i === messages.length - 1 && m.role === 'assistant' && (
                        <span className="inline-block w-1.5 h-4 bg-gray-400 ml-1 animate-pulse" />
                      )}
                    </div>
                  </div>
                ))}
                <div ref={chatBottomRef} />
              </div>

              <div className="px-4 py-4 border-t border-gray-800 flex gap-3">
                <input
                  value={input}
                  onChange={e => setInput(e.target.value)}
                  onKeyDown={e => e.key === 'Enter' && !e.shiftKey && sendMessage()}
                  placeholder="Ask your coach…"
                  maxLength={2000}
                  disabled={streaming}
                  className="flex-1 bg-gray-800 border border-gray-700 rounded-xl px-4 py-2.5 text-white placeholder-gray-500 text-sm focus:outline-none focus:border-indigo-500 disabled:opacity-50"
                />
                <button onClick={sendMessage} disabled={!input.trim() || streaming}
                  className="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-40 text-white px-4 py-2.5 rounded-xl transition-colors text-sm font-medium">
                  Send
                </button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
