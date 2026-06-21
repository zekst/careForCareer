import { useState, useRef, type DragEvent, type ChangeEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { createProfile, updateProfile } from '../api/candidate'
import { uploadResume } from '../api/resume'
import { searchJobs, getSuggestedJobs, type Job } from '../api/jobs'
import PositioningPanel from '../components/PositioningPanel'

const TIER_LABELS = ['Fresh Grad', 'Junior (SDE-1)', 'Mid-level (SDE-2)', 'Senior (SDE-3)', 'Staff+']
const TIER_COLORS = ['bg-gray-500', 'bg-blue-500', 'bg-emerald-500', 'bg-purple-500', 'bg-amber-500']

export default function OnboardingPage() {
  const [step, setStep] = useState<1 | 2 | 3>(1)

  // Step 1
  const [yoe, setYoe] = useState(0)
  const [company, setCompany] = useState('')
  const [currentComp, setCurrentComp] = useState(0)
  const [targetComp, setTargetComp] = useState(0)
  const [tier, setTier] = useState<number | null>(null)
  const [tierExplanation, setTierExplanation] = useState('')
  const [saving, setSaving] = useState(false)

  // Step 2
  const [dragging, setDragging] = useState(false)
  const [file, setFile] = useState<File | null>(null)
  const [uploading, setUploading] = useState(false)
  const [uploadDone, setUploadDone] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  // Step 3 – job search
  const [jobQuery, setJobQuery] = useState('')
  const [jobLocation, setJobLocation] = useState('')
  const [searching, setSearching] = useState(false)
  const [jobs, setJobs] = useState<Job[]>([])
  const [selectedJob, setSelectedJob] = useState<Job | null>(null)
  const [searchDone, setSearchDone] = useState(false)

  const [error, setError] = useState('')
  const navigate = useNavigate()

  // ─── Step 1: save profile ─────────────────────────────────────────────────
  const saveProfile = async () => {
    setError('')
    if (targetComp > 0 && currentComp > 0 && targetComp <= currentComp) {
      setError('Target CTC should be higher than your current CTC')
      return
    }
    setSaving(true)
    try {
      let res
      try {
        res = await updateProfile({ years_experience: yoe, current_company: company, current_comp_inr: currentComp, target_comp_inr: targetComp })
      } catch (updateErr: any) {
        if (updateErr.response?.status !== 404) throw updateErr
        res = await createProfile({ years_experience: yoe, current_company: company, current_comp_inr: currentComp, target_comp_inr: targetComp })
      }
      setTier(res.data.tier)
      setTierExplanation(res.data.tier_explanation)
      setStep(2)
    } catch (err: any) {
      setError(err.response?.data?.error?.message ?? 'Failed to save profile')
    } finally {
      setSaving(false)
    }
  }

  const formatLPA = (inr: number) => inr > 0 ? `= ₹${(inr / 100000).toFixed(1)}L` : ''

  // ─── Step 2: resume upload ────────────────────────────────────────────────
  const MAX_RESUME_BYTES = 5 * 1024 * 1024

  const handleDrop = (e: DragEvent) => {
    e.preventDefault()
    setDragging(false)
    const f = e.dataTransfer.files[0]
    if (!f) return
    if (f.type !== 'application/pdf') { setError('Only PDF files are accepted'); return }
    if (f.size === 0) { setError('The file appears to be empty'); return }
    if (f.size > MAX_RESUME_BYTES) { setError('File is too large — maximum size is 5 MB'); return }
    setError('')
    setFile(f)
  }

  const handleFileChange = (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0]
    if (!f) return
    if (f.type !== 'application/pdf') { setError('Only PDF files are accepted'); return }
    if (f.size === 0) { setError('The file appears to be empty'); return }
    if (f.size > MAX_RESUME_BYTES) { setError('File is too large — maximum size is 5 MB'); return }
    setError('')
    setFile(f)
  }

  const handleUpload = async () => {
    if (!file) return
    setUploading(true)
    setError('')
    try {
      await uploadResume(file)
      setUploadDone(true)
    } catch (err: any) {
      setError(err.response?.data?.error?.message ?? 'Upload failed')
    } finally {
      setUploading(false)
    }
  }

  // ─── Step 3: job search ───────────────────────────────────────────────────
  const handleJobSearch = async () => {
    if (!jobQuery.trim()) return
    setSearching(true)
    setError('')
    setJobs([])
    try {
      const res = await searchJobs(jobQuery.trim(), jobLocation.trim())
      setJobs(res.data.jobs ?? [])
      setSearchDone(true)
    } catch (err: any) {
      setError(err.response?.data?.error?.message ?? 'Job search failed')
    } finally {
      setSearching(false)
    }
  }

  const handleSuggestJobs = async () => {
    setSearching(true)
    setError('')
    setJobs([])
    try {
      const res = await getSuggestedJobs()
      setJobs(res.data.jobs ?? [])
      setSearchDone(true)
      if (res.data.query) setJobQuery(res.data.query)
    } catch (err: any) {
      setError(err.response?.data?.error?.message ?? 'Could not load suggestions')
    } finally {
      setSearching(false)
    }
  }

  const selectJob = (job: Job) => {
    setSelectedJob(job)
  }

  const goToDashboard = () => {
    if (selectedJob?.description) {
      const params = new URLSearchParams({
        jd: selectedJob.description,
        job_title: selectedJob.title,
        company: selectedJob.company,
        location: selectedJob.location,
      })
      navigate(`/dashboard?${params.toString()}`)
    } else {
      navigate('/dashboard')
    }
  }

  // ─── Render ───────────────────────────────────────────────────────────────
  const totalSteps = 3
  return (
    <div className="min-h-screen bg-gray-950 text-white px-4 py-12">
      <div className="max-w-2xl mx-auto">
        {/* Header + progress */}
        <div className="mb-8">
          <h1 className="text-2xl font-bold">Set up your careForCareer profile</h1>
          <div className="flex gap-2 mt-4">
            {Array.from({ length: totalSteps }, (_, i) => (
              <div key={i} className={`h-1.5 flex-1 rounded-full ${step > i ? 'bg-indigo-500' : 'bg-gray-700'}`} />
            ))}
          </div>
          <p className="text-gray-500 text-sm mt-2">Step {step} of {totalSteps}</p>
        </div>

        {/* ── Step 1: Experience ─────────────────────────────────────────── */}
        {step === 1 && (
          <div className="bg-gray-900 rounded-2xl p-8 border border-gray-800 space-y-5">
            <h2 className="text-lg font-semibold">Your experience</h2>

            <div>
              <label className="block text-sm text-gray-400 mb-1">Years of experience</label>
              <input type="number" min={0} max={40} value={yoe}
                onChange={e => setYoe(Number(e.target.value))}
                className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-indigo-500" />
            </div>
            <div>
              <label className="block text-sm text-gray-400 mb-1">Current company <span className="text-gray-600">(optional)</span></label>
              <input type="text" value={company} onChange={e => setCompany(e.target.value)}
                placeholder="e.g. Amazon, Infosys, Startup"
                className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2.5 text-white placeholder-gray-500 focus:outline-none focus:border-indigo-500" />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-sm text-gray-400 mb-1">
                  Current CTC <span className="text-gray-600">(₹ — e.g. 1200000 = 12 LPA)</span>
                </label>
                <input type="number" min={0} value={currentComp || ''} onChange={e => setCurrentComp(Number(e.target.value))}
                  placeholder="e.g. 1200000"
                  className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2.5 text-white placeholder-gray-600 focus:outline-none focus:border-indigo-500" />
                {currentComp > 0 && (
                  <p className="text-xs text-indigo-400 mt-1">{formatLPA(currentComp)}</p>
                )}
              </div>
              <div>
                <label className="block text-sm text-gray-400 mb-1">
                  Target CTC <span className="text-gray-600">(₹ — must be &gt; current)</span>
                </label>
                <input type="number" min={0} value={targetComp || ''} onChange={e => setTargetComp(Number(e.target.value))}
                  placeholder="e.g. 2000000"
                  className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2.5 text-white placeholder-gray-600 focus:outline-none focus:border-indigo-500" />
                {targetComp > 0 && (
                  <p className={`text-xs mt-1 ${targetComp > currentComp || currentComp === 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                    {formatLPA(targetComp)}
                    {currentComp > 0 && targetComp <= currentComp ? ' — must exceed current CTC' : ''}
                  </p>
                )}
              </div>
            </div>

            {error && <div className="bg-red-900/40 border border-red-700 rounded-lg px-4 py-2.5 text-red-300 text-sm">{error}</div>}

            <button onClick={saveProfile} disabled={saving}
              className="w-full bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white font-medium rounded-lg py-2.5 transition-colors">
              {saving ? 'Saving…' : 'Continue →'}
            </button>
          </div>
        )}

        {/* ── Step 2: Resume upload ──────────────────────────────────────── */}
        {step === 2 && (
          <div className="space-y-6">
            {tier !== null && (
              <div className="bg-gray-900 rounded-2xl p-6 border border-gray-800 flex items-center gap-4">
                <span className={`${TIER_COLORS[tier]} text-white text-sm font-semibold px-3 py-1 rounded-full`}>
                  {TIER_LABELS[tier]}
                </span>
                <p className="text-gray-400 text-sm">{tierExplanation}</p>
              </div>
            )}

            <div className="bg-gray-900 rounded-2xl p-8 border border-gray-800 space-y-5">
              <h2 className="text-lg font-semibold">Upload your resume</h2>
              <p className="text-gray-400 text-sm">We'll extract your skills and calibrate your readiness score. PDF only, max 5 MB.</p>

              <div
                onDragOver={e => { e.preventDefault(); setDragging(true) }}
                onDragLeave={() => setDragging(false)}
                onDrop={handleDrop}
                onClick={() => fileRef.current?.click()}
                className={`border-2 border-dashed rounded-xl p-10 text-center cursor-pointer transition-colors
                  ${dragging ? 'border-indigo-400 bg-indigo-900/20' : 'border-gray-700 hover:border-gray-500'}`}
              >
                <input ref={fileRef} type="file" accept=".pdf" className="hidden" onChange={handleFileChange} />
                {file ? (
                  <p className="text-indigo-300 font-medium">📄 {file.name}</p>
                ) : (
                  <>
                    <p className="text-gray-400">Drag & drop your PDF here</p>
                    <p className="text-gray-600 text-sm mt-1">or click to browse</p>
                  </>
                )}
              </div>

              {error && <div className="bg-red-900/40 border border-red-700 rounded-lg px-4 py-2.5 text-red-300 text-sm">{error}</div>}

              {!uploadDone ? (
                <div className="space-y-2">
                  <button onClick={handleUpload} disabled={!file || uploading}
                    className="w-full bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white font-medium rounded-lg py-2.5 transition-colors">
                    {uploading ? 'Uploading…' : 'Upload resume'}
                  </button>
                  <button onClick={() => setStep(3)}
                    className="w-full text-gray-500 hover:text-gray-300 border border-gray-700 hover:border-gray-600 rounded-lg py-2 text-sm transition-colors">
                    Skip for now →
                  </button>
                </div>
              ) : (
                <div className="space-y-3">
                  <div className="bg-emerald-900/40 border border-emerald-700 rounded-lg px-4 py-2.5 text-emerald-300 text-sm">
                    ✓ Resume uploaded successfully
                  </div>
                  <button onClick={() => setStep(3)}
                    className="w-full bg-indigo-600 hover:bg-indigo-500 text-white font-medium rounded-lg py-2.5 transition-colors">
                    Find target jobs →
                  </button>
                </div>
              )}
            </div>
          </div>
        )}

        {/* ── Step 3: Job search ─────────────────────────────────────────── */}
        {step === 3 && (
          <div className="space-y-6">
            <div className="bg-gray-900 rounded-2xl p-8 border border-gray-800 space-y-5">
              <div>
                <div className="flex items-center gap-2">
                  <button onClick={() => setStep(2)} className="text-gray-500 hover:text-gray-300 text-sm transition-colors">← Back</button>
                  <h2 className="text-lg font-semibold">Find your target role</h2>
                  <div className="flex gap-1.5">
                    <span className="text-xs bg-blue-900/50 text-blue-300 border border-blue-800 px-2 py-0.5 rounded-full font-medium">LinkedIn</span>
                    <span className="text-xs bg-gray-700 text-gray-500 border border-gray-600 px-2 py-0.5 rounded-full" title="Coming soon">Naukri</span>
                    <span className="text-xs bg-gray-700 text-gray-500 border border-gray-600 px-2 py-0.5 rounded-full" title="Coming soon">Wellfound</span>
                  </div>
                </div>
                <p className="text-gray-400 text-sm mt-1">Click a role to instantly see where you stand and what to improve.</p>
              </div>

              {/* Suggest button */}
              <button
                onClick={handleSuggestJobs}
                disabled={searching}
                className="w-full bg-emerald-700 hover:bg-emerald-600 disabled:opacity-50 text-white px-4 py-2.5 rounded-lg text-sm font-medium transition-colors flex items-center justify-center gap-2"
              >
                {searching ? (
                  <span className="flex items-center gap-2">
                    <span className="w-3.5 h-3.5 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                    Finding jobs for you…
                  </span>
                ) : '✨ Get suggested jobs based on my profile'}
              </button>

              <div className="flex items-center gap-3 text-gray-600 text-xs">
                <div className="flex-1 h-px bg-gray-700" />
                or search manually
                <div className="flex-1 h-px bg-gray-700" />
              </div>

              {/* Search inputs */}
              <div className="flex gap-3">
                <input
                  type="text"
                  value={jobQuery}
                  onChange={e => setJobQuery(e.target.value)}
                  onKeyDown={e => e.key === 'Enter' && handleJobSearch()}
                  placeholder="Role e.g. Backend Engineer"
                  maxLength={100}
                  className="flex-1 bg-gray-800 border border-gray-700 rounded-lg px-4 py-2.5 text-white placeholder-gray-500 text-sm focus:outline-none focus:border-indigo-500"
                />
                <input
                  type="text"
                  value={jobLocation}
                  onChange={e => setJobLocation(e.target.value)}
                  onKeyDown={e => e.key === 'Enter' && handleJobSearch()}
                  placeholder="Location e.g. Bangalore"
                  className="w-40 bg-gray-800 border border-gray-700 rounded-lg px-4 py-2.5 text-white placeholder-gray-500 text-sm focus:outline-none focus:border-indigo-500"
                />
                <button
                  onClick={handleJobSearch}
                  disabled={!jobQuery.trim() || searching}
                  className="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white px-5 py-2.5 rounded-lg text-sm font-medium transition-colors whitespace-nowrap"
                >
                  {searching ? '…' : 'Search'}
                </button>
              </div>

              {error && <div className="bg-red-900/40 border border-red-700 rounded-lg px-4 py-2.5 text-red-300 text-sm">{error}</div>}

              {/* Job listings */}
              {searchDone && jobs.length === 0 && (
                <p className="text-gray-500 text-sm text-center py-6">No jobs found. Try a different keyword.</p>
              )}

              {jobs.length > 0 && (
                <div className="space-y-3 max-h-96 overflow-y-auto pr-1">
                  {jobs.map(job => (
                    <button
                      key={job.id}
                      onClick={() => selectJob(job)}
                      className={`w-full text-left rounded-xl p-4 border transition-all
                        ${selectedJob?.id === job.id
                          ? 'border-indigo-500 bg-indigo-900/30'
                          : 'border-gray-700 bg-gray-800 hover:border-gray-600 hover:bg-gray-750'}`}
                    >
                      <div className="flex items-start justify-between gap-3">
                        <div className="flex-1 min-w-0">
                          <p className="font-medium text-white text-sm truncate">{job.title}</p>
                          <p className="text-gray-400 text-xs mt-0.5">{job.company}</p>
                          <p className="text-gray-500 text-xs mt-0.5">📍 {job.location}</p>
                        </div>
                        <div className="flex flex-col items-end gap-1.5 shrink-0">
                          {selectedJob?.id === job.id && (
                            <span className="text-indigo-400 text-xs font-semibold">✓ Selected</span>
                          )}
                          <a
                            href={job.apply_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            onClick={e => e.stopPropagation()}
                            className="text-xs text-gray-500 hover:text-indigo-400 transition-colors"
                          >
                            Apply ↗
                          </a>
                          {job.source === 'mock' && (
                            <span className="text-xs text-gray-600 bg-gray-700 px-1.5 py-0.5 rounded">Demo</span>
                          )}
                        </div>
                      </div>
                      {selectedJob?.id === job.id && job.description && (
                        <p className="text-gray-500 text-xs mt-2" style={{ display: '-webkit-box', WebkitLineClamp: 3, WebkitBoxOrient: 'vertical', overflow: 'hidden' }}>{job.description}</p>
                      )}
                    </button>
                  ))}
                </div>
              )}

              {/* Positioning panel — shown immediately after selecting a job */}
              {selectedJob && (
                <div className="pt-1">
                  <PositioningPanel job={selectedJob} compact />
                </div>
              )}

              {/* CTA */}
              <div className="flex gap-3 pt-1">
                {selectedJob ? (
                  <button
                    onClick={goToDashboard}
                    className="flex-1 bg-indigo-600 hover:bg-indigo-500 text-white font-medium rounded-lg py-2.5 transition-colors text-sm"
                  >
                    Go to full analysis →
                  </button>
                ) : (
                  <button
                    onClick={() => navigate('/dashboard')}
                    className="flex-1 text-gray-500 hover:text-gray-300 border border-gray-700 hover:border-gray-600 rounded-lg py-2.5 transition-colors text-sm"
                  >
                    Skip — paste JD manually
                  </button>
                )}
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
