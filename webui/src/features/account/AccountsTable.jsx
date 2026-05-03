import { useState } from 'react'
import {
    Check,
    ChevronLeft,
    ChevronRight,
    Copy,
    FolderX,
    Loader2,
    Pencil,
    Play,
    Plus,
    Search,
    Trash2,
} from 'lucide-react'
import clsx from 'clsx'

function StatusBadge({ acc, isActive, runtimeUnknown, t }) {
    const failed = acc.test_status === 'failed'
    const label = failed
        ? t('accountManager.testStatusFailed')
        : isActive
            ? t('accountManager.sessionActive')
            : runtimeUnknown
                ? t('accountManager.runtimeStatusUnknown')
                : t('accountManager.reauthRequired')

    return (
        <span className={clsx(
            'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-[11px] font-bold',
            failed
                ? 'border-red-300/60 bg-red-50 text-red-700'
                : isActive
                    ? 'border-emerald-300/60 bg-emerald-50 text-emerald-700'
                    : runtimeUnknown
                        ? 'border-blue-300/60 bg-blue-50 text-blue-700'
                        : 'border-amber-300/60 bg-amber-50 text-amber-700',
        )}>
            <span className={clsx(
                'status-dot',
                failed ? 'status-dot-danger' : isActive ? 'status-dot-ok' : 'status-dot-warn',
            )} />
            {label}
        </span>
    )
}

export default function AccountsTable({
    t,
    accounts,
    loadingAccounts,
    testing,
    testingAll,
    batchProgress,
    sessionCounts,
    deletingSessions,
    updatingProxy,
    totalAccounts,
    page,
    pageSize,
    totalPages,
    resolveAccountIdentifier,
    proxies,
    onTestAll,
    onShowAddAccount,
    onEditAccount,
    onTestAccount,
    onDeleteAccount,
    onDeleteAllSessions,
    onUpdateAccountProxy,
    onPrevPage,
    onNextPage,
    onPageSizeChange,
    searchQuery,
    onSearchChange,
    envBacked = false,
}) {
    const [copiedId, setCopiedId] = useState(null)
    const showBatchProgress = batchProgress.total > 0 && (testingAll || batchProgress.results.length > 0)
    const batchSuccessCount = batchProgress.results.filter(result => result.success).length
    const batchProgressPercent = batchProgress.total > 0
        ? (batchProgress.current / batchProgress.total) * 100
        : 0

    const copyId = (id) => {
        navigator.clipboard.writeText(id).then(() => {
            setCopiedId(id)
            setTimeout(() => setCopiedId(null), 1500)
        })
    }

    return (
        <div className="ops-panel overflow-hidden">
            <div className="border-b border-border px-4 py-3">
                <div className="flex flex-col xl:flex-row xl:items-center justify-between gap-3">
                    <div>
                        <p className="ops-kicker">Managed Accounts</p>
                        <div className="mt-1 flex flex-wrap items-center gap-2">
                            <h2 className="ops-heading">{t('accountManager.accountsTitle')}</h2>
                            <span className="rounded-md border border-border bg-muted/60 px-2 py-0.5 text-[11px] font-black tabular-nums text-muted-foreground">
                                {totalAccounts}
                            </span>
                        </div>
                        <p className="ops-subtle mt-0.5">{t('accountManager.accountsDesc')}</p>
                    </div>
                    <div className="flex flex-col sm:flex-row gap-2 sm:items-center xl:min-w-[520px] 2xl:min-w-[680px]">
                        <div className="relative min-w-[260px] flex-1">
                            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                            <input
                                type="text"
                                value={searchQuery}
                                onChange={e => onSearchChange(e.target.value)}
                                placeholder={t('accountManager.searchPlaceholder')}
                                className="input-field pl-9"
                            />
                        </div>
                        <button
                            onClick={onTestAll}
                            disabled={testingAll || totalAccounts === 0}
                            className="btn btn-secondary"
                        >
                            {testingAll ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Play className="w-3.5 h-3.5" />}
                            {t('accountManager.testAll')}
                        </button>
                        <button
                            onClick={onShowAddAccount}
                            className="btn btn-primary"
                        >
                            <Plus className="w-4 h-4" />
                            {t('accountManager.addAccount')}
                        </button>
                    </div>
                </div>
            </div>

            {showBatchProgress && (
                <div className="border-b border-border bg-blue-50/70 px-4 py-3 page-transition">
                    <div className="flex items-center justify-between text-sm mb-2">
                        <span className="font-bold">
                            {testingAll
                                ? t('accountManager.testingAllAccounts')
                                : t('accountManager.testAllCompleted', { success: batchSuccessCount, total: batchProgress.total })}
                        </span>
                        <span className="text-muted-foreground tabular-nums">{batchProgress.current} / {batchProgress.total}</span>
                    </div>
                    <div className="w-full bg-white rounded-full h-1.5 overflow-hidden mb-3 border border-blue-100">
                        <div
                            className="bg-primary h-full transition-all duration-300 ease-out"
                            style={{ width: `${batchProgressPercent}%` }}
                        />
                    </div>
                    {batchProgress.results.length > 0 && (
                        <div className="grid grid-cols-2 md:grid-cols-4 xl:grid-cols-6 gap-2 max-h-28 overflow-y-auto custom-scrollbar">
                            {batchProgress.results.map((r, i) => (
                                <div key={i} className={clsx(
                                    'text-xs px-2 py-1 rounded-md border truncate font-mono transition-transform hover:-translate-y-0.5',
                                    r.success ? 'bg-emerald-50 border-emerald-300/60 text-emerald-700' : 'bg-red-50 border-red-300/60 text-red-700',
                                )}>
                                    {r.success ? 'OK' : 'ERR'} {r.id}
                                </div>
                            ))}
                        </div>
                    )}
                </div>
            )}

            <div className="overflow-x-auto">
                <div className="min-w-[1080px]">
                    <div className="grid grid-cols-[minmax(360px,2.3fr)_minmax(150px,0.55fr)_minmax(130px,0.45fr)_minmax(260px,0.9fr)_minmax(190px,0.7fr)] gap-3 border-b border-border bg-slate-50 px-4 py-2 text-[11px] font-black uppercase text-muted-foreground">
                        <div>Account</div>
                        <div>Status</div>
                        <div>Sessions</div>
                        <div>Proxy</div>
                        <div className="text-right">Actions</div>
                    </div>

                    {loadingAccounts ? (
                        <div className="px-4 py-5 space-y-3">
                            {[0, 1, 2, 3].map(i => (
                                <div key={i} className="grid grid-cols-[minmax(360px,2.3fr)_minmax(150px,0.55fr)_minmax(130px,0.45fr)_minmax(260px,0.9fr)_minmax(190px,0.7fr)] gap-3 items-center">
                                    <div className="space-y-2">
                                        <div className="h-3 w-44 rounded-full skeleton-line" />
                                        <div className="h-2.5 w-64 rounded-full skeleton-line" />
                                    </div>
                                    <div className="h-7 rounded-full skeleton-line" />
                                    <div className="h-7 rounded-md skeleton-line" />
                                    <div className="h-8 rounded-md skeleton-line" />
                                    <div className="h-8 rounded-md skeleton-line" />
                                </div>
                            ))}
                        </div>
                    ) : accounts.length > 0 ? (
                        accounts.map((acc, i) => {
                            const id = resolveAccountIdentifier(acc)
                            const assignedProxy = proxies.find(proxy => proxy.id === acc.proxy_id)
                            const runtimeUnknown = envBacked && !acc.test_status
                            const isActive = acc.test_status === 'ok' || acc.has_token
                            const sessionCount = sessionCounts?.[id] ?? acc.session_count
                            return (
                                <div
                                    key={i}
                                    className="page-transition table-row-hover grid grid-cols-[minmax(360px,2.3fr)_minmax(150px,0.55fr)_minmax(130px,0.45fr)_minmax(260px,0.9fr)_minmax(190px,0.7fr)] gap-3 items-center border-b border-border/70 px-4 py-3 last:border-b-0"
                                    style={{ animationDelay: `${Math.min(i, 10) * 18}ms` }}
                                >
                                    <div className="min-w-0">
                                        <div className="flex items-center gap-2">
                                            <span className={clsx(
                                                'status-dot',
                                                acc.test_status === 'failed' ? 'status-dot-danger' : isActive ? 'status-dot-ok' : 'status-dot-warn',
                                            )} />
                                            <span className="text-sm font-black truncate">{acc.name || '-'}</span>
                                        </div>
                                        <button
                                            className="mt-1 max-w-full inline-flex items-center gap-1.5 font-mono text-xs text-muted-foreground hover:text-primary transition-colors"
                                            onClick={() => copyId(id)}
                                        >
                                            <span className="truncate">{id || '-'}</span>
                                            {copiedId === id
                                                ? <Check className="w-3 h-3 text-emerald-600 shrink-0" />
                                                : <Copy className="w-3 h-3 opacity-50 shrink-0" />
                                            }
                                        </button>
                                        {acc.remark && (
                                            <div className="mt-1 text-xs text-muted-foreground truncate">{acc.remark}</div>
                                        )}
                                        {acc.token_preview && (
                                            <div className="mt-1 inline-flex font-mono bg-muted px-1.5 py-0.5 rounded text-[10px] text-muted-foreground">
                                                {acc.token_preview}
                                            </div>
                                        )}
                                    </div>

                                    <div>
                                        <StatusBadge acc={acc} isActive={isActive} runtimeUnknown={runtimeUnknown} t={t} />
                                    </div>

                                    <div>
                                        {sessionCount !== undefined ? (
                                            <div className="flex items-center gap-2">
                                                <span className="rounded-md border border-blue-300/60 bg-blue-50 px-2 py-1 text-[11px] font-black text-blue-700 tabular-nums">
                                                    {t('accountManager.sessionCount', { count: sessionCount })}
                                                </span>
                                                {sessionCount > 0 && (
                                                    <button
                                                        onClick={() => onDeleteAllSessions(id)}
                                                        disabled={deletingSessions?.[id]}
                                                        className="p-1.5 rounded-md text-red-600 hover:bg-red-50 disabled:opacity-50 transition-colors"
                                                        title={t('accountManager.deleteAllSessions')}
                                                    >
                                                        {deletingSessions?.[id] ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <FolderX className="w-3.5 h-3.5" />}
                                                    </button>
                                                )}
                                            </div>
                                        ) : (
                                            <span className="text-xs text-muted-foreground">-</span>
                                        )}
                                    </div>

                                    <div>
                                        <select
                                            value={acc.proxy_id || ''}
                                            onChange={e => onUpdateAccountProxy(id, e.target.value)}
                                            disabled={updatingProxy?.[id]}
                                            className="input-field h-8 min-h-8 py-1 text-xs"
                                        >
                                            <option value="">{t('accountManager.proxyNone')}</option>
                                            {proxies.map(proxy => (
                                                <option key={proxy.id} value={proxy.id}>
                                                    {proxy.name || `${proxy.host}:${proxy.port}`}
                                                </option>
                                            ))}
                                        </select>
                                        {acc.proxy_id && (
                                            <div className="mt-1 truncate text-[10px] font-mono text-amber-700">
                                                {assignedProxy ? (assignedProxy.name || `${assignedProxy.host}:${assignedProxy.port}`) : acc.proxy_id}
                                            </div>
                                        )}
                                    </div>

                                    <div className="flex items-center justify-end gap-1.5">
                                        <button
                                            onClick={() => onEditAccount(acc)}
                                            disabled={!id}
                                            className="btn btn-secondary btn-sm px-2"
                                            title={id ? t('accountManager.editAccountTitle') : t('accountManager.invalidIdentifier')}
                                        >
                                            <Pencil className="w-3.5 h-3.5" />
                                        </button>
                                        <button
                                            onClick={() => onTestAccount(id)}
                                            disabled={testing[id]}
                                            className="btn btn-secondary btn-sm"
                                        >
                                            {testing[id] ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : null}
                                            {testing[id] ? t('actions.testing') : t('actions.test')}
                                        </button>
                                        <button
                                            onClick={() => onDeleteAccount(id)}
                                            className="btn btn-danger btn-sm px-2"
                                            title={t('actions.delete')}
                                        >
                                            <Trash2 className="w-3.5 h-3.5" />
                                        </button>
                                    </div>
                                </div>
                            )
                        })
                    ) : (
                        <div className="px-4 py-14 text-center">
                            <div className="mx-auto mb-3 flex h-11 w-11 items-center justify-center rounded-full border border-slate-200 bg-slate-50 text-muted-foreground">
                                <Search className="w-5 h-5" />
                            </div>
                            <div className="text-sm font-semibold text-muted-foreground">
                                {searchQuery ? t('accountManager.searchNoResults') : t('accountManager.noAccounts')}
                            </div>
                        </div>
                    )}
                </div>
            </div>

            {totalPages > 1 && (
                <div className="border-t border-border px-4 py-3 flex flex-col sm:flex-row sm:items-center justify-between gap-3 bg-muted/25">
                    <div className="flex items-center gap-3">
                        <div className="text-sm font-semibold text-muted-foreground">
                            {t('accountManager.pageInfo', { current: page, total: totalPages, count: totalAccounts })}
                        </div>
                        <select
                            value={pageSize}
                            onChange={e => onPageSizeChange(Number(e.target.value))}
                            className="input-field h-8 min-h-8 w-24 py-1 text-xs"
                        >
                            {[10, 20, 50, 100, 500, 1000, 2000, 5000].map(s => (
                                <option key={s} value={s}>{s}</option>
                            ))}
                        </select>
                    </div>
                    <div className="flex items-center gap-2">
                        <button
                            onClick={onPrevPage}
                            disabled={page <= 1 || loadingAccounts}
                            className="btn btn-secondary btn-sm px-2"
                        >
                            <ChevronLeft className="w-4 h-4" />
                        </button>
                        <span className="text-sm font-black px-2 tabular-nums">{page} / {totalPages}</span>
                        <button
                            onClick={onNextPage}
                            disabled={page >= totalPages || loadingAccounts}
                            className="btn btn-secondary btn-sm px-2"
                        >
                            <ChevronRight className="w-4 h-4" />
                        </button>
                    </div>
                </div>
            )}
        </div>
    )
}
