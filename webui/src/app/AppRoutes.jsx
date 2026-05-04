import { useCallback } from 'react'
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import clsx from 'clsx'

import LandingPage from '../components/LandingPage'
import Login from '../components/Login'
import DashboardShell from '../layout/DashboardShell'
import { useI18n } from '../i18n'
import { useAdminAuth } from './useAdminAuth'
import { useAdminConfig } from './useAdminConfig'

export default function AppRoutes() {
    const { t } = useI18n()
    const navigate = useNavigate()
    const location = useLocation()

    const isProduction = import.meta.env.MODE === 'production'
    const {
        token,
        authChecking,
        message,
        isAdminRoute,
        showMessage,
        handleLogin,
        handleLogout,
        handleAuthExpired,
    } = useAdminAuth({ isProduction, location, t })

    const authFetch = useCallback(async (url, options = {}) => {
        if (!token) {
            handleAuthExpired()
            const error = new Error(t('auth.expired'))
            error.authExpired = true
            throw error
        }

        const headers = new Headers(options.headers || {})
        headers.set('Authorization', `Bearer ${token}`)
        const res = await fetch(url, { ...options, headers })

        if (res.status === 401 || res.status === 403) {
            handleAuthExpired()
            const error = new Error(t('auth.expired'))
            error.authExpired = true
            throw error
        }

        return res
    }, [handleAuthExpired, t, token])

    const {
        config,
        fetchConfig,
    } = useAdminConfig({ token, authFetch, showMessage, t })

    if (isAdminRoute && authChecking) {
        return (
            <div className="min-h-screen flex items-center justify-center bg-background">
                <div className="flex flex-col items-center gap-4">
                    <div className="w-8 h-8 border-4 border-primary border-t-transparent rounded-full animate-spin"></div>
                    <p className="text-muted-foreground animate-pulse">{t('auth.checking')}</p>
                </div>
            </div>
        )
    }

    return (
        <Routes>
            {!isProduction && (
                <Route path="/" element={<LandingPage onEnter={() => navigate('/admin')} />} />
            )}
            <Route path={isProduction ? "/*" : "/admin/*"} element={
                token ? (
                    <DashboardShell
                        onLogout={handleLogout}
                        authFetch={authFetch}
                        config={config}
                        fetchConfig={fetchConfig}
                        showMessage={showMessage}
                        message={message}
                        onForceLogout={handleLogout}
                    />
                ) : (
                    <div className="min-h-screen flex flex-col bg-background relative overflow-hidden">
                        {message && (
                            <div className={clsx(
                                "fixed top-4 right-4 z-50 px-4 py-3 rounded-lg shadow-lg border animate-in slide-in-from-top-2 fade-in",
                                message.type === 'error' ? "bg-red-50 border-destructive/20 text-destructive" :
                                    "bg-emerald-50 border-primary/20 text-primary"
                            )}>
                                {message.text}
                            </div>
                        )}
                        <Login onLogin={handleLogin} onMessage={showMessage} />
                    </div>
                )
            } />
            <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
    )
}
