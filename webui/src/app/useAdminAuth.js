import { useCallback, useEffect, useState } from 'react'

export function useAdminAuth({ isProduction, location, t }) {
    const [message, setMessage] = useState(null)
    const [token, setToken] = useState(null)
    const [authChecking, setAuthChecking] = useState(true)

    const isAdminRoute = location.pathname.startsWith('/admin') || isProduction

    const showMessage = useCallback((type, text) => {
        setMessage({ type, text })
        setTimeout(() => setMessage(null), 5000)
    }, [])

    const handleLogout = useCallback(() => {
        setToken(null)
        sessionStorage.removeItem('deepseek-web-to-api_token')
        sessionStorage.removeItem('deepseek-web-to-api_token_expires')
    }, [])

    const handleLogin = useCallback((newToken) => {
        setToken(newToken)
    }, [])

    useEffect(() => {
        if (!isAdminRoute) {
            setAuthChecking(false)
            return
        }

        const checkAuth = async () => {
            const storedToken = sessionStorage.getItem('deepseek-web-to-api_token')
            const expiresAt = parseInt(sessionStorage.getItem('deepseek-web-to-api_token_expires') || '0')

            if (storedToken && expiresAt > Date.now()) {
                try {
                    const res = await fetch('/admin/verify', {
                        headers: { 'Authorization': `Bearer ${storedToken}` }
                    })
                    if (res.ok) {
                        setToken(storedToken)
                    } else {
                        handleLogout()
                    }
                } catch {
                    setToken(storedToken)
                }
            }
            setAuthChecking(false)
        }

        checkAuth()
    }, [handleLogout, isAdminRoute, t])

    return {
        token,
        authChecking,
        message,
        isAdminRoute,
        showMessage,
        handleLogin,
        handleLogout,
    }
}
