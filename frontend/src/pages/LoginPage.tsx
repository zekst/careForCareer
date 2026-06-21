import { useState, type FormEvent } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { login, register } from '../api/auth'
import { useAuth } from '../context/AuthContext'

export default function LoginPage({ mode }: { mode: 'login' | 'register' }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const { signIn } = useAuth()
  const navigate = useNavigate()

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const res = mode === 'login'
        ? await login(email, password)
        : await register(email, password)
      const { access_token, refresh_token, user_id } = res.data
      signIn(user_id ?? '', access_token, refresh_token)
      navigate('/onboarding')
    } catch (err: any) {
      setError(err.response?.data?.error?.message ?? 'Something went wrong')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-gray-950 flex items-center justify-center px-4">
      <div className="w-full max-w-md">
        <div className="mb-8 text-center">
          <h1 className="text-3xl font-bold text-white">careForCareer</h1>
          <p className="text-gray-400 mt-2">Your AI-powered interview readiness coach</p>
        </div>

        <div className="bg-gray-900 rounded-2xl p-8 border border-gray-800">
          <h2 className="text-xl font-semibold text-white mb-6">
            {mode === 'login' ? 'Sign in to your account' : 'Create your account'}
          </h2>

          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label className="block text-sm text-gray-400 mb-1">Email</label>
              <input
                type="email"
                value={email}
                onChange={e => setEmail(e.target.value)}
                required
                className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2.5 text-white placeholder-gray-500 focus:outline-none focus:border-indigo-500"
                placeholder="you@example.com"
              />
            </div>
            <div>
              <label className="block text-sm text-gray-400 mb-1">Password</label>
              <input
                type="password"
                value={password}
                onChange={e => setPassword(e.target.value)}
                required
                minLength={8}
                maxLength={72}
                className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2.5 text-white placeholder-gray-500 focus:outline-none focus:border-indigo-500"
                placeholder="••••••••"
              />
            </div>

            {error && (
              <div className="bg-red-900/40 border border-red-700 rounded-lg px-4 py-2.5 text-red-300 text-sm">
                {error}
              </div>
            )}

            <button
              type="submit"
              disabled={loading}
              className="w-full bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white font-medium rounded-lg py-2.5 transition-colors"
            >
              {loading ? 'Please wait…' : mode === 'login' ? 'Sign in' : 'Create account'}
            </button>
          </form>

          <p className="text-center text-gray-500 text-sm mt-6">
            {mode === 'login' ? (
              <>Don't have an account?{' '}
                <Link to="/register" className="text-indigo-400 hover:text-indigo-300">Sign up</Link>
              </>
            ) : (
              <>Already have an account?{' '}
                <Link to="/login" className="text-indigo-400 hover:text-indigo-300">Sign in</Link>
              </>
            )}
          </p>
        </div>
      </div>
    </div>
  )
}
