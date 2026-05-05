import { useCallback, useEffect, useRef, useState } from 'react'

export function useAdminAuth({ isProduction, location, t }) {
    const [message, setMessage] = useState(null)
    const [token, setToken] = useState(null)
    const [authChecking, setAuthChecking] = useState(true)
    const authExpiredNotifiedRef = useRef(false)

    const isAdminRoute = location.pathname.startsWith('/admin') || isProduction

    const showMessage = useCallback((type, text) => {
        setMessage({ type, text })
        setTimeout(() => setMessage(null), 5000)
    }, [])

    // The admin JWT is persisted in localStorage rather than sessionStorage so
    // that hard refreshes (Ctrl+Shift+R / Firefox refreshes / restored tabs)
    // do not lose the token and dispatch unauthenticated requests, which
    // surface as {"detail":"authentication required"} in the WebUI. See
    // cnb.cool/Neko_Kernel/DeepSeek_Web_To_API#9. We still also clear the
    // legacy sessionStorage entries on logout / expiry so older browser
    // sessions get migrated cleanly.
    const clearStoredCredentials = () => {
        localStorage.removeItem('deepseek-web-to-api_token')
        localStorage.removeItem('deepseek-web-to-api_token_expires')
        sessionStorage.removeItem('deepseek-web-to-api_token')
        sessionStorage.removeItem('deepseek-web-to-api_token_expires')
    }

    const handleLogout = useCallback(() => {
        authExpiredNotifiedRef.current = false
        setToken(null)
        clearStoredCredentials()
    }, [])

    const handleLogin = useCallback((newToken) => {
        authExpiredNotifiedRef.current = false
        setToken(newToken)
    }, [])

    const handleAuthExpired = useCallback(() => {
        setToken(null)
        clearStoredCredentials()
        if (!authExpiredNotifiedRef.current) {
            authExpiredNotifiedRef.current = true
            showMessage('error', t('auth.expired'))
        }
    }, [showMessage, t])

    useEffect(() => {
        if (!isAdminRoute) {
            setAuthChecking(false)
            return
        }

        const checkAuth = async () => {
            // Prefer localStorage (current persistence). Fall back to
            // sessionStorage so sessions established before this fix migrate
            // automatically on the first successful refresh.
            let storedToken = localStorage.getItem('deepseek-web-to-api_token')
            let expiresAt = parseInt(localStorage.getItem('deepseek-web-to-api_token_expires') || '0')
            if (!storedToken) {
                storedToken = sessionStorage.getItem('deepseek-web-to-api_token')
                expiresAt = parseInt(sessionStorage.getItem('deepseek-web-to-api_token_expires') || '0')
                if (storedToken && expiresAt > Date.now()) {
                    localStorage.setItem('deepseek-web-to-api_token', storedToken)
                    localStorage.setItem('deepseek-web-to-api_token_expires', String(expiresAt))
                    sessionStorage.removeItem('deepseek-web-to-api_token')
                    sessionStorage.removeItem('deepseek-web-to-api_token_expires')
                }
            }

            if (storedToken && expiresAt > Date.now()) {
                try {
                    const res = await fetch('/admin/verify', {
                        headers: { 'Authorization': `Bearer ${storedToken}` }
                    })
                    if (res.ok) {
                        setToken(storedToken)
                    } else {
                        handleAuthExpired()
                    }
                } catch {
                    setToken(storedToken)
                }
            }
            setAuthChecking(false)
        }

        checkAuth()
    }, [handleAuthExpired, isAdminRoute])

    return {
        token,
        authChecking,
        message,
        isAdminRoute,
        showMessage,
        handleLogin,
        handleLogout,
        handleAuthExpired,
    }
}
