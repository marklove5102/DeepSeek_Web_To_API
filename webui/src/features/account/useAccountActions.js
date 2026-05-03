import { useState } from 'react'

export function useAccountActions({ apiFetch, t, onMessage, onRefresh, config, totalAccounts, fetchAccounts, resolveAccountIdentifier }) {
    const [showAddKey, setShowAddKey] = useState(false)
    const [editingKey, setEditingKey] = useState(null)
    const [showAddAccount, setShowAddAccount] = useState(false)
    const [showEditAccount, setShowEditAccount] = useState(false)
    const [editingAccount, setEditingAccount] = useState(null)
    const [newKey, setNewKey] = useState({ key: '', name: '', remark: '' })
    const [copiedKey, setCopiedKey] = useState(null)
    const [newAccount, setNewAccount] = useState({ name: '', remark: '', email: '', mobile: '', password: '' })
    const [editAccount, setEditAccount] = useState({ name: '', remark: '' })
    const [loading, setLoading] = useState(false)
    const [testing, setTesting] = useState({})
    const [testingAll, setTestingAll] = useState(false)
    const [batchProgress, setBatchProgress] = useState({ current: 0, total: 0, results: [] })
    const [sessionCounts, setSessionCounts] = useState({})
    const [deletingSessions, setDeletingSessions] = useState({})
    const [updatingProxy, setUpdatingProxy] = useState({})

    const readJSONResponse = async (res) => {
        const text = await res.text()
        if (!text.trim()) return {}
        try {
            return JSON.parse(text)
        } catch (_err) {
            return { detail: text.trim() }
        }
    }

    const fetchAllAccountsForBatch = async () => {
        const pageSize = 5000
        let currentPage = 1
        let totalPages = 1
        const allAccounts = []

        while (currentPage <= totalPages) {
            const res = await apiFetch(`/admin/accounts?page=${currentPage}&page_size=${pageSize}`)
            const data = await readJSONResponse(res)
            if (!res.ok) {
                throw new Error(data.detail || data.message || t('messages.requestFailed'))
            }
            const items = Array.isArray(data.items) ? data.items : []
            allAccounts.push(...items)
            totalPages = Math.max(1, Number(data.total_pages) || 1)
            currentPage += 1
        }

        return allAccounts
    }

    const openAddKey = () => {
        setEditingKey(null)
        setNewKey({ key: '', name: '', remark: '' })
        setShowAddKey(true)
    }

    const openEditKey = (item) => {
        if (!item?.key) return
        setEditingKey(item)
        setNewKey({
            key: item.key || '',
            name: item.name || '',
            remark: item.remark || '',
        })
        setShowAddKey(true)
    }

    const closeKeyModal = () => {
        setShowAddKey(false)
        setEditingKey(null)
        setNewKey({ key: '', name: '', remark: '' })
    }

    const openAddAccount = () => {
        setShowEditAccount(false)
        setEditingAccount(null)
        setEditAccount({ name: '', remark: '' })
        setNewAccount({ name: '', remark: '', email: '', mobile: '', password: '' })
        setShowAddAccount(true)
    }

    const closeAddAccount = () => {
        setShowAddAccount(false)
        setNewAccount({ name: '', remark: '', email: '', mobile: '', password: '' })
    }

    const openEditAccount = (account) => {
        const identifier = resolveAccountIdentifier(account)
        if (!identifier) {
            onMessage('error', t('accountManager.invalidIdentifier'))
            return
        }
        setShowAddAccount(false)
        setEditingAccount({
            identifier,
        })
        setEditAccount({
            name: account?.name || '',
            remark: account?.remark || '',
        })
        setShowEditAccount(true)
    }

    const closeEditAccount = () => {
        setShowEditAccount(false)
        setEditingAccount(null)
        setEditAccount({ name: '', remark: '' })
    }

    const addKey = async () => {
        const isEditing = Boolean(editingKey?.key)
        if (!isEditing && !newKey.key.trim()) {
            return
        }
        setLoading(true)
        try {
            const endpoint = isEditing
                ? `/admin/keys/${encodeURIComponent(editingKey.key)}`
                : '/admin/keys'
            const method = isEditing ? 'PUT' : 'POST'
            const payload = isEditing
                ? { name: newKey.name, remark: newKey.remark }
                : { key: newKey.key.trim(), name: newKey.name, remark: newKey.remark }
            if (!isEditing && !payload.key) {
                return
            }
            const res = await apiFetch(endpoint, {
                method,
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload),
            })
            if (res.ok) {
                onMessage('success', isEditing ? t('accountManager.updateKeySuccess') : t('accountManager.addKeySuccess'))
                closeKeyModal()
                onRefresh()
            } else {
                const data = await res.json()
                onMessage('error', data.detail || (isEditing ? t('messages.requestFailed') : t('messages.failedToAdd')))
            }
        } catch (e) {
            onMessage('error', t('messages.networkError'))
        } finally {
            setLoading(false)
        }
    }

    const deleteKey = async (key) => {
        if (!confirm(t('accountManager.deleteKeyConfirm'))) return
        try {
            const res = await apiFetch(`/admin/keys/${encodeURIComponent(key)}`, { method: 'DELETE' })
            if (res.ok) {
                onMessage('success', t('messages.deleted'))
                onRefresh()
            } else {
                onMessage('error', t('messages.deleteFailed'))
            }
        } catch (e) {
            onMessage('error', t('messages.networkError'))
        }
    }

    const addAccount = async () => {
        if (!newAccount.password || (!newAccount.email && !newAccount.mobile)) {
            onMessage('error', t('accountManager.requiredFields'))
            return
        }
        setLoading(true)
        try {
            const res = await apiFetch('/admin/accounts', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(newAccount),
            })
            if (res.ok) {
                onMessage('success', t('accountManager.addAccountSuccess'))
                closeAddAccount()
                fetchAccounts(1)
                onRefresh()
            } else {
                const data = await res.json()
                onMessage('error', data.detail || t('messages.failedToAdd'))
            }
        } catch (e) {
            onMessage('error', t('messages.networkError'))
        } finally {
            setLoading(false)
        }
    }

    const updateAccount = async () => {
        const identifier = String(editingAccount?.identifier || '').trim()
        if (!identifier) {
            onMessage('error', t('accountManager.invalidIdentifier'))
            return
        }
        setLoading(true)
        try {
            const res = await apiFetch(`/admin/accounts/${encodeURIComponent(identifier)}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(editAccount),
            })
            if (res.ok) {
                onMessage('success', t('accountManager.updateAccountSuccess'))
                closeEditAccount()
                fetchAccounts()
                onRefresh()
            } else {
                const data = await res.json()
                onMessage('error', data.detail || t('messages.requestFailed'))
            }
        } catch (e) {
            onMessage('error', t('messages.networkError'))
        } finally {
            setLoading(false)
        }
    }

    const deleteAccount = async (id) => {
        const identifier = String(id || '').trim()
        if (!identifier) {
            onMessage('error', t('accountManager.invalidIdentifier'))
            return
        }
        if (!confirm(t('accountManager.deleteAccountConfirm'))) return
        try {
            const res = await apiFetch(`/admin/accounts/${encodeURIComponent(identifier)}`, { method: 'DELETE' })
            if (res.ok) {
                onMessage('success', t('messages.deleted'))
                fetchAccounts()
                onRefresh()
            } else {
                onMessage('error', t('messages.deleteFailed'))
            }
        } catch (e) {
            onMessage('error', t('messages.networkError'))
        }
    }

    const testAccount = async (identifier) => {
        const accountID = String(identifier || '').trim()
        if (!accountID) {
            onMessage('error', t('accountManager.invalidIdentifier'))
            return
        }
        setTesting(prev => ({ ...prev, [accountID]: true }))
        try {
            const res = await apiFetch('/admin/accounts/test', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ identifier: accountID }),
            })
            const data = await res.json()
            
            // 更新会话数
            if (data.session_count !== undefined) {
                setSessionCounts(prev => ({ ...prev, [accountID]: data.session_count }))
            }
            
            const statusMessage = data.success
                ? t('apiTester.testSuccess', { account: accountID, time: data.response_time })
                : `${accountID}: ${data.message}`
            onMessage(data.success ? 'success' : 'error', statusMessage)
            fetchAccounts()
            onRefresh()
        } catch (e) {
            onMessage('error', t('accountManager.testFailed', { error: e.message }))
        } finally {
            setTesting(prev => ({ ...prev, [accountID]: false }))
        }
    }

    const testAllAccounts = async () => {
        if (!confirm(t('accountManager.testAllConfirm'))) return
        const expectedTotal = totalAccounts || config?.accounts?.length || 0
        if (expectedTotal === 0) return

        setTestingAll(true)
        setBatchProgress({ current: 0, total: expectedTotal, results: [] })

        try {
            const allAccounts = await fetchAllAccountsForBatch()
            if (allAccounts.length === 0) {
                setBatchProgress({ current: 0, total: 0, results: [] })
                return
            }

            const total = allAccounts.length
            const results = []
            let completed = 0
            let successCount = 0
            let lastTableRefreshAt = 0

            setBatchProgress({ current: 0, total, results: [] })

            const refreshVisibleAccounts = () => {
                const now = Date.now()
                if (completed !== total && now - lastTableRefreshAt < 1500) return
                lastTableRefreshAt = now
                fetchAccounts()
            }

            const recordResult = (result) => {
                results.push(result)
                completed += 1
                if (result.success) successCount += 1
                if (result.sessionCount !== undefined && result.id !== '-') {
                    setSessionCounts(prev => ({ ...prev, [result.id]: result.sessionCount }))
                }
                setBatchProgress({ current: completed, total, results: [...results] })
                refreshVisibleAccounts()
            }

            const testOne = async (acc) => {
                const id = resolveAccountIdentifier(acc)
                if (!id) {
                    return { id: '-', success: false, message: t('accountManager.invalidIdentifier') }
                }
                try {
                    const res = await apiFetch('/admin/accounts/test', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ identifier: id, model: 'deepseek-v4-flash' }),
                    })
                    const data = await readJSONResponse(res)
                    return {
                        id,
                        success: res.ok && Boolean(data.success),
                        message: data.message || data.detail || '',
                        time: data.response_time,
                        sessionCount: data.session_count,
                    }
                } catch (e) {
                    return { id, success: false, message: e.message }
                }
            }

            let nextIndex = 0
            const workerCount = Math.min(5, total)
            const workers = Array.from({ length: workerCount }, async () => {
                while (nextIndex < total) {
                    const account = allAccounts[nextIndex]
                    nextIndex += 1
                    recordResult(await testOne(account))
                }
            })

            await Promise.all(workers)

            await fetchAccounts()
            await onRefresh()
            onMessage('success', t('accountManager.testAllCompleted', { success: successCount, total }))
        } catch (e) {
            onMessage('error', t('accountManager.testFailed', { error: e.message }))
        } finally {
            setTestingAll(false)
        }
    }

    const deleteAllSessions = async (identifier) => {
        const accountID = String(identifier || '').trim()
        if (!accountID) {
            onMessage('error', t('accountManager.invalidIdentifier'))
            return
        }
        if (!confirm(t('accountManager.deleteAllSessionsConfirm'))) return
        
        setDeletingSessions(prev => ({ ...prev, [accountID]: true }))
        try {
            const res = await apiFetch('/admin/accounts/sessions/delete-all', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ identifier: accountID }),
            })
            const data = await res.json()
            
            if (data.success) {
                onMessage('success', t('accountManager.deleteAllSessionsSuccess'))
                setSessionCounts(prev => ({ ...prev, [accountID]: 0 }))
                fetchAccounts()
            } else {
                onMessage('error', data.message || t('messages.requestFailed'))
            }
        } catch (e) {
            onMessage('error', t('messages.networkError'))
        } finally {
            setDeletingSessions(prev => ({ ...prev, [accountID]: false }))
        }
    }

    const updateAccountProxy = async (identifier, proxyID) => {
        const accountID = String(identifier || '').trim()
        if (!accountID) {
            onMessage('error', t('accountManager.invalidIdentifier'))
            return
        }
        setUpdatingProxy(prev => ({ ...prev, [accountID]: true }))
        try {
            const res = await apiFetch(`/admin/accounts/${encodeURIComponent(accountID)}/proxy`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ proxy_id: proxyID || '' }),
            })
            const data = await res.json()
            if (!res.ok) {
                onMessage('error', data.detail || t('messages.requestFailed'))
                return
            }
            onMessage('success', t('accountManager.proxyUpdateSuccess'))
            fetchAccounts()
            onRefresh()
        } catch (_err) {
            onMessage('error', t('messages.networkError'))
        } finally {
            setUpdatingProxy(prev => ({ ...prev, [accountID]: false }))
        }
    }

    return {
        showAddKey,
        openAddKey,
        openEditKey,
        closeKeyModal,
        editingKey,
        showAddAccount,
        openAddAccount,
        closeAddAccount,
        showEditAccount,
        editingAccount,
        editAccount,
        setEditAccount,
        openEditAccount,
        closeEditAccount,
        newKey,
        setNewKey,
        copiedKey,
        setCopiedKey,
        newAccount,
        setNewAccount,
        loading,
        testing,
        testingAll,
        batchProgress,
        sessionCounts,
        deletingSessions,
        updatingProxy,
        addKey,
        deleteKey,
        addAccount,
        updateAccount,
        deleteAccount,
        testAccount,
        testAllAccounts,
        deleteAllSessions,
        updateAccountProxy,
    }
}
