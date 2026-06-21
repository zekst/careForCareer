import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'
import { analysePivot, type PivotResult, type PivotSkill, type EntryPath } from '../api/pivot'

const TARGET_ROLES = [
  { v: 'Program Manager', label: '📋 Program Manager (PM)' },
  { v: 'Technical Program Manager', label: '⚙️ Technical Program Manager (TPM)' },
  { v: 'Engineering Manager', label: '👥 Engineering Manager (EM)' },
  { v: 'Site Reliability Engineer', label: '🔧 SRE / DevOps' },
  { v: 'Data Scientist', label: '📊 Data Scientist / ML Engineer' },
  { v: 'Solutions Architect', label: '🏗 Solutions Architect' },
  { v: 'Developer Advocate', label: '🎤 Developer Advocate / DevRel' },
  { v: 'Product Designer', label: '🎨 Product Designer' },
  { v: 'Other', label: '✏️ Other — I\'ll type it' },
]

export default function PivotPage() {
  const navigate = useNavigate()
  const { isAuthenticated } = useAuth()

  const [targetRole, setTargetRole] = useState('')
  const [targetRoleCustom, setTargetRoleCustom] = useState('')
  const [currentRole, setCurrentRole] = useState('')
  const [jd, setJD] = useState('')
  const [currentSkills, setCurrentSkills] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [result, setResult] = useState<PivotResult | null>(null)

  const handleAnalyse = async () => {
    const role = targetRole === 'Other' ? targetRoleCustom : targetRole
    if (!role) return
    setLoading(true)
    setError('')
    try {
      const res = await analysePivot({
        target_role: role,
        current_role: currentRole,
        job_description: jd,
        current_skills: currentSkills.split(',').map(s => s.trim()).filter(Boolean),
      })
      setResult(res.data)
    } catch (err: any) {
      setError(err.response?.data?.error?.message ?? 'Analysis failed. Try again.')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-gray-950 text-white">
      {/* Header */}
      <div className="border-b border-gray-800 px-6 py-4 flex items-center justify-between">
        <button onClick={() => navigate(isAuthenticated ? '/dashboard' : '/')} className="flex items-center gap-2 text-gray-400 hover:text-white transition-colors text-sm">
          ← {isAuthenticated ? 'Dashboard' : 'Home'}
        </button>
        <span className="text-xs bg-purple-900/50 text-purple-300 border border-purple-800 px-2.5 py-1 rounded-full font-medium">🔄 Career Pivot</span>
        <div />
      </div>

      <div className="max-w-2xl mx-auto px-4 py-8 space-y-6">
        {!result ? (
          <>
            <div className="text-center space-y-2">
              <h1 className="text-2xl font-bold">Career Pivot Analysis</h1>
              <p className="text-gray-400 text-sm">Where does your engineering background take you? Get an honest assessment of your pivot.</p>
            </div>

            {/* Target role picker */}
            <div className="space-y-3">
              <label className="text-sm font-medium text-gray-300">What role do you want to pivot to?</label>
              <div className="grid grid-cols-2 gap-2">
                {TARGET_ROLES.map(r => (
                  <button key={r.v} onClick={() => setTargetRole(r.v)}
                    className={`text-left px-3 py-2.5 rounded-lg border text-sm transition-all
                      ${targetRole === r.v
                        ? 'bg-purple-900/50 border-purple-600 text-purple-200'
                        : 'bg-gray-800/60 border-gray-700 text-gray-300 hover:border-gray-500'}`}>
                    {r.label}
                  </button>
                ))}
              </div>
              {targetRole === 'Other' && (
                <input
                  autoFocus
                  value={targetRoleCustom}
                  onChange={e => setTargetRoleCustom(e.target.value)}
                  placeholder="e.g. Strategy Consultant, VC Associate..."
                  maxLength={100}
                  className="w-full bg-gray-800 border border-gray-600 rounded-lg px-3 py-2.5 text-white placeholder-gray-500 text-sm focus:outline-none focus:border-purple-500"
                />
              )}
            </div>

            {/* Optional context */}
            <div className="space-y-3">
              <label className="text-sm font-medium text-gray-300">Your current role <span className="text-gray-500">(optional)</span></label>
              <input
                value={currentRole}
                onChange={e => setCurrentRole(e.target.value)}
                placeholder="e.g. SDE-2 at Amazon, Backend engineer at a startup..."
                maxLength={200}
                className="w-full bg-gray-800 border border-gray-600 rounded-lg px-3 py-2.5 text-white placeholder-gray-500 text-sm focus:outline-none focus:border-purple-500"
              />
            </div>

            <div className="space-y-3">
              <label className="text-sm font-medium text-gray-300">Your skills <span className="text-gray-500">(optional — comma separated)</span></label>
              <input
                value={currentSkills}
                onChange={e => setCurrentSkills(e.target.value)}
                placeholder="e.g. Java, distributed systems, AWS, system design..."
                maxLength={500}
                className="w-full bg-gray-800 border border-gray-600 rounded-lg px-3 py-2.5 text-white placeholder-gray-500 text-sm focus:outline-none focus:border-purple-500"
              />
            </div>

            <div className="space-y-3">
              <label className="text-sm font-medium text-gray-300">
                Paste a JD <span className="text-gray-500">(optional — makes analysis much more specific)</span>
              </label>
              <textarea
                value={jd}
                onChange={e => setJD(e.target.value)}
                rows={5}
                maxLength={20000}
                placeholder="Paste the job description for a specific role you're targeting..."
                className="w-full bg-gray-800 border border-gray-600 rounded-lg px-3 py-2.5 text-white placeholder-gray-500 text-sm focus:outline-none focus:border-purple-500 resize-none"
              />
            </div>

            {error && (
              <div className="bg-red-900/30 border border-red-800/50 rounded-lg px-4 py-3 text-red-300 text-sm">
                {error}
              </div>
            )}

            <button
              onClick={handleAnalyse}
              disabled={loading || (!targetRole || (targetRole === 'Other' && !targetRoleCustom))}
              className="w-full bg-purple-600 hover:bg-purple-500 disabled:opacity-40 text-white font-semibold rounded-xl py-4 text-sm transition-colors"
            >
              {loading ? (
                <span className="flex items-center justify-center gap-2">
                  <span className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                  Analysing your pivot…
                </span>
              ) : 'Analyse my pivot →'}
            </button>
          </>
        ) : (
          <PivotResultView result={result} onReset={() => setResult(null)} />
        )}
      </div>
    </div>
  )
}

// ── Pivot result display ───────────────────────────────────────────────────────

function PivotResultView({ result, onReset }: { result: PivotResult; onReset: () => void }) {
  const [tab, setTab] = useState<'overview' | 'skills' | 'paths' | 'plan'>('overview')

  const difficultyStyle: Record<string, string> = {
    hard: 'bg-red-900/40 text-red-300 border-red-800',
    moderate: 'bg-yellow-900/40 text-yellow-300 border-yellow-800',
    easy: 'bg-emerald-900/40 text-emerald-300 border-emerald-800',
  }

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="bg-gray-800 rounded-xl p-5 border border-gray-700 space-y-3">
        <div className="flex items-start justify-between">
          <div>
            <p className="text-xs text-gray-400 uppercase tracking-wider mb-1">Starting fit</p>
            <div className="flex items-baseline gap-2">
              <span className="text-4xl font-bold text-purple-400">{result.overall_fit}%</span>
              <span className="text-gray-500 text-sm">before any prep</span>
            </div>
          </div>
          <span className={`text-xs px-2.5 py-1 rounded-full border font-medium ${difficultyStyle[result.pivot_difficulty] ?? ''}`}>
            {result.difficulty_label}
          </span>
        </div>
        <div className="h-2 bg-gray-700 rounded-full overflow-hidden">
          <div className="h-full bg-purple-500 rounded-full" style={{ width: `${result.overall_fit}%` }} />
        </div>
        <p className="text-gray-300 text-sm leading-relaxed">{result.summary}</p>
        <div className="flex gap-4 pt-1">
          <div>
            <p className="text-xs text-gray-500">Optimistic timeline</p>
            <p className="text-white text-sm font-semibold">{result.timeline.optimistic}</p>
          </div>
          <div>
            <p className="text-xs text-gray-500">Realistic timeline</p>
            <p className="text-white text-sm font-semibold">{result.timeline.realistic}</p>
          </div>
        </div>
      </div>

      {/* Day in the life */}
      <div className="bg-indigo-900/20 border border-indigo-800/40 rounded-xl p-4">
        <p className="text-xs text-indigo-400 font-semibold uppercase tracking-wider mb-1">📅 A day in the life of {result.target_role}</p>
        <p className="text-gray-300 text-sm leading-relaxed">{result.day_in_the_life}</p>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 bg-gray-800/60 rounded-xl p-1 border border-gray-700 overflow-x-auto">
        {(['overview', 'skills', 'paths', 'plan'] as const).map(t => (
          <button key={t} onClick={() => setTab(t)}
            className={`flex-1 py-2 rounded-lg text-xs font-semibold transition-colors whitespace-nowrap px-2
              ${tab === t ? 'bg-purple-600 text-white' : 'text-gray-400 hover:text-gray-200'}`}>
            {t === 'overview' ? '📋 Overview' : t === 'skills' ? '🛠 Skills' : t === 'paths' ? '🛤 Entry paths' : '📅 Prep plan'}
          </button>
        ))}
      </div>

      {/* Overview tab */}
      {tab === 'overview' && (
        <div className="space-y-3">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div className="bg-gray-800 rounded-xl p-4 border border-gray-700">
              <p className="text-xs text-gray-400 mb-1">⚡ What helps</p>
              <p className="text-gray-200 text-sm">{result.timeline.what_helps}</p>
            </div>
            <div className="bg-gray-800 rounded-xl p-4 border border-gray-700">
              <p className="text-xs text-gray-400 mb-1">⚠️ What hurts</p>
              <p className="text-gray-200 text-sm">{result.timeline.what_hurts}</p>
            </div>
          </div>

          <div className="bg-gray-800 rounded-xl p-4 border border-gray-700 space-y-2">
            <p className="text-xs text-gray-400 uppercase tracking-wider">💬 Honest caveats</p>
            {result.honest_caveats.map((c, i) => (
              <div key={i} className="flex items-start gap-2 text-sm text-gray-300 py-1 border-b border-gray-700/50 last:border-0">
                <span className="text-gray-600 shrink-0 mt-0.5">•</span> {c}
              </div>
            ))}
          </div>

          <div className="bg-amber-900/20 border border-amber-800/40 rounded-xl p-4 space-y-2">
            <p className="text-xs text-amber-400 font-semibold uppercase tracking-wider">⚡ Quick wins — start this week</p>
            {result.quick_wins.map((w, i) => (
              <div key={i} className="flex items-start gap-2 text-sm text-amber-200">
                <span className="text-amber-500 shrink-0 font-bold">{i + 1}.</span> {w}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Skills tab */}
      {tab === 'skills' && (
        <div className="space-y-4">
          <div className="bg-gray-800 rounded-xl p-4 border border-gray-700 space-y-3">
            <p className="text-xs font-semibold text-emerald-400 uppercase tracking-wider">✅ What transfers (your edge)</p>
            {result.transferable_skills.map((s, i) => <SkillRow key={i} skill={s} />)}
          </div>
          <div className="bg-gray-800 rounded-xl p-4 border border-gray-700 space-y-3">
            <p className="text-xs font-semibold text-red-400 uppercase tracking-wider">📚 What you need to learn</p>
            {result.skills_to_learn.map((s, i) => <SkillRow key={i} skill={s} />)}
          </div>
        </div>
      )}

      {/* Entry paths tab */}
      {tab === 'paths' && (
        <div className="space-y-3">
          {result.entry_paths.map((p, i) => <EntryPathCard key={i} path={p} />)}
        </div>
      )}

      {/* Prep plan tab */}
      {tab === 'plan' && (
        <div className="space-y-3">
          {result.prep_plan_outline.map((phase, i) => (
            <div key={i} className="bg-gray-800 rounded-xl p-4 border border-gray-700 space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-white font-semibold text-sm">{phase.phase}</span>
                <span className="text-xs text-gray-500">Weeks {phase.weeks}</span>
              </div>
              <p className="text-gray-400 text-xs">{phase.goal}</p>
              <div className="flex flex-wrap gap-1.5">
                {phase.topics.map(t => (
                  <span key={t} className="text-xs bg-purple-900/30 text-purple-300 border border-purple-800/50 px-2 py-0.5 rounded-full">{t}</span>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}

      <button onClick={onReset} className="text-xs text-gray-600 hover:text-gray-400 transition-colors">
        ↩ New analysis
      </button>
    </div>
  )
}

function SkillRow({ skill }: { skill: PivotSkill }) {
  const relevanceColor: Record<string, string> = {
    high: 'text-emerald-400',
    medium: 'text-yellow-400',
    low: 'text-gray-500',
  }
  return (
    <div className="flex items-start gap-3 py-1.5 border-b border-gray-700/50 last:border-0">
      <span className={`text-xs font-bold uppercase mt-0.5 shrink-0 w-10 ${relevanceColor[skill.relevance] ?? ''}`}>
        {skill.relevance}
      </span>
      <div>
        <p className="text-white text-sm font-medium">{skill.skill}</p>
        <p className="text-gray-400 text-xs mt-0.5">{skill.comment}</p>
      </div>
    </div>
  )
}

function EntryPathCard({ path }: { path: EntryPath }) {
  const diffColor: Record<string, string> = {
    high: 'text-red-400 bg-red-900/30 border-red-800/50',
    medium: 'text-yellow-400 bg-yellow-900/30 border-yellow-800/50',
    low: 'text-emerald-400 bg-emerald-900/30 border-emerald-800/50',
  }
  return (
    <div className="bg-gray-800 rounded-xl p-4 border border-gray-700 space-y-2">
      <div className="flex items-start justify-between gap-2">
        <p className="text-white font-semibold text-sm">{path.name}</p>
        <span className={`text-xs px-2 py-0.5 rounded-full border ${diffColor[path.difficulty] ?? ''}`}>
          {path.difficulty} difficulty
        </span>
      </div>
      <p className="text-gray-300 text-xs leading-relaxed">{path.description}</p>
      {path.examples.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {path.examples.map(e => (
            <span key={e} className="text-xs bg-gray-700 text-gray-300 px-2 py-0.5 rounded">{e}</span>
          ))}
        </div>
      )}
      <p className="text-xs text-gray-500">Best for: {path.best_for}</p>
    </div>
  )
}
