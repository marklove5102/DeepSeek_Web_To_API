import { useState } from 'react'
import { Activity, ArrowRight, Key, Lock, ShieldCheck } from 'lucide-react'
import { useI18n } from '../i18n'
import LanguageToggle from './LanguageToggle'

async function readLoginResponse(res) {
    const text = await res.text()
    if (!text.trim()) {
        throw new Error(`HTTP ${res.status} ${res.statusText || 'Empty response'} from /admin/login`)
    }

    try {
        return JSON.parse(text)
    } catch {
        throw new Error(`HTTP ${res.status} returned non-JSON response from /admin/login`)
    }
}

export default function Login({ onLogin, onMessage }) {
    const { t } = useI18n()
    const [adminKey, setAdminKey] = useState('')
    const [loading, setLoading] = useState(false)

    const handleLogin = async (e) => {
        e.preventDefault()
        if (!adminKey.trim()) return

        setLoading(true)

        try {
            const res = await fetch('/admin/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ admin_key: adminKey }),
            })

            const data = await readLoginResponse(res)

            if (res.ok && data.success) {
                // Use localStorage so that the JWT survives full page refreshes
                // (sessionStorage was cleared by Firefox/some browsers on hard
                // refresh, leaving the SPA to dispatch unauthenticated requests
                // and triggering {"detail":"authentication required"}). See
                // cnb.cool/Neko_Kernel/DeepSeek_Web_To_API#9.
                localStorage.setItem('deepseek-web-to-api_token', data.token)
                localStorage.setItem('deepseek-web-to-api_token_expires', Date.now() + data.expires_in * 1000)

                onLogin(data.token)
                if (data.message) {
                    onMessage('warning', data.message)
                }
            } else {
                onMessage('error', data.detail || t('login.signInFailed'))
            }
        } catch (e) {
            onMessage('error', t('login.networkError', { error: e.message }))
        } finally {
            setLoading(false)
        }
    }

    return (
        <div className="ops-shell relative min-h-screen w-full overflow-hidden text-foreground">
            <div className="absolute top-5 right-5 z-20">
                <LanguageToggle />
            </div>

            <div className="min-h-screen grid lg:grid-cols-[minmax(500px,1.02fr)_minmax(420px,0.98fr)]">
                <section className="hidden lg:flex relative overflow-hidden border-r border-slate-200 bg-white text-slate-950">
                    <div className="absolute inset-0 ops-login-scene" />
                    <div className="absolute inset-x-0 bottom-0 h-36 bg-[linear-gradient(180deg,transparent,rgba(255,255,255,0.92))]" />

                    <div className="relative z-10 flex min-h-screen w-full flex-col justify-between p-10">
                        <div className="inline-flex items-center gap-3">
                            <div>
                                <div className="text-2xl font-black leading-none">DeepSeek_Web_To_API</div>
                                <div className="mt-1 text-[11px] font-black uppercase text-slate-500">Operations Console</div>
                            </div>
                        </div>

                        <div className="max-w-xl page-transition">
                            <div className="inline-flex items-center gap-2 rounded-md border border-blue-200 bg-blue-50 px-3 py-1.5 text-xs font-bold text-blue-700 backdrop-blur-md">
                                <Activity className="w-4 h-4" />
                                Running
                            </div>
                            <h1 className="mt-6 text-5xl font-black leading-[1.04]">
                                DeepSeek_Web_To_API 管理控制台
                            </h1>
                            <p className="mt-5 max-w-lg text-base leading-7 text-slate-600">
                                统一管理账号池、API Key、代理与流式调用状态。
                            </p>
                            <div className="ops-status-card mt-8 max-w-md">
                                <div>
                                    <div className="text-[10px] font-black uppercase text-blue-600">DeepSeek_Web_To_API 运维助手</div>
                                    <div className="mt-1 text-sm font-black text-slate-900">Gateway online</div>
                                    <div className="mt-2 grid grid-cols-3 gap-2 text-[11px] text-slate-600">
                                        <span className="rounded-md border border-blue-100 bg-white/80 px-2 py-1">SSE</span>
                                        <span className="rounded-md border border-emerald-100 bg-white/80 px-2 py-1">Pool</span>
                                        <span className="rounded-md border border-amber-100 bg-white/80 px-2 py-1">Proxy</span>
                                    </div>
                                </div>
                            </div>
                        </div>

                        <div className="grid grid-cols-3 gap-3">
                            {[
                                ['SSE', 'Ready'],
                                ['Pool', 'Affinity'],
                                ['Proxy', 'Managed'],
                            ].map(([label, value]) => (
                                <div key={label} className="ops-mini-stat p-3 transition-transform duration-200 hover:-translate-y-1">
                                    <div className="text-[10px] font-black uppercase text-slate-500">{label}</div>
                                    <div className="mt-2 text-sm font-bold text-slate-900">{value}</div>
                                </div>
                            ))}
                        </div>
                    </div>
                </section>

                <section className="flex min-h-screen items-center justify-center px-5 py-12 lg:px-12">
                    <div className="w-full max-w-[430px] page-transition">
                        <div className="lg:hidden mb-8 flex items-center gap-3">
                            <div>
                                <div className="text-xl font-black">DeepSeek_Web_To_API</div>
                                <div className="text-[10px] font-black uppercase text-muted-foreground">Operations Console</div>
                            </div>
                        </div>

                        <div className="ops-panel p-6 lg:p-7">
                            <div className="mb-7">
                                <div className="inline-flex items-center justify-center w-10 h-10 rounded-lg border border-blue-100 bg-blue-50 text-blue-600 mb-4">
                                    <Lock className="w-5 h-5" />
                                </div>
                                <h1 className="text-2xl font-black text-foreground">{t('login.welcome')}</h1>
                                <p className="mt-2 text-sm text-muted-foreground">{t('login.subtitle')}</p>
                            </div>

                            <form onSubmit={handleLogin} className="space-y-5">
                                <div className="space-y-2">
                                    <label className="ops-kicker block">{t('login.adminKeyLabel')}</label>
                                    <div className="relative group">
                                        <Key className="input-icon-left absolute top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground group-focus-within:text-primary transition-colors" />
                                        <input
                                            type="password"
                                            className="input-field input-field-with-icon h-11"
                                            placeholder={t('login.adminKeyPlaceholder')}
                                            value={adminKey}
                                            onChange={e => setAdminKey(e.target.value)}
                                            autoFocus
                                        />
                                    </div>
                                </div>

                                <button
                                    type="submit"
                                    disabled={loading}
                                    className="btn btn-primary w-full h-11 text-sm"
                                >
                                    {loading ? (
                                        <div className="w-5 h-5 border-2 border-primary-foreground/30 border-t-primary-foreground rounded-full animate-spin" />
                                    ) : (
                                        <>
                                            <span>{t('login.signIn')}</span>
                                            <ArrowRight className="w-4 h-4" />
                                        </>
                                    )}
                                </button>
                            </form>

                            <div className="mt-6 pt-5 border-t border-border flex items-center justify-between gap-3">
                                <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground font-bold uppercase">
                                    <ShieldCheck className="w-3.5 h-3.5 text-primary" />
                                    <span>{t('login.secureConnection')}</span>
                                </div>
                                <span className="text-[10px] text-muted-foreground font-mono">{t('login.adminPortal')}</span>
                            </div>
                        </div>
                    </div>
                </section>
            </div>
        </div>
    )
}
