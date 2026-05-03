import { CheckCircle2, Clock3, Gauge, Server, ShieldCheck } from 'lucide-react'

function QueueMetric({ icon: Icon, label, value, unit, tone = 'ok' }) {
    return (
        <div className="metric-tile group">
            <div className="flex items-start justify-between gap-3">
                <div>
                    <p className="ops-kicker">{label}</p>
                    <div className="mt-2 flex items-baseline gap-2">
                        <span className="text-3xl font-black tracking-tight tabular-nums text-foreground">{value}</span>
                        {unit && <span className="text-xs font-bold text-muted-foreground">{unit}</span>}
                    </div>
                </div>
                <div className="w-9 h-9 rounded-lg border border-blue-100 bg-blue-50 flex items-center justify-center text-primary transition-transform duration-200 group-hover:scale-105">
                    <Icon className="w-4 h-4" />
                </div>
            </div>
            <div className="mt-4 inline-flex items-center gap-1.5 text-[11px] font-bold text-muted-foreground">
                <span className={tone === 'warn' ? 'status-dot status-dot-warn' : 'status-dot status-dot-ok'} />
                <span>{tone === 'warn' ? '存在等待' : '状态正常'}</span>
            </div>
        </div>
    )
}

export default function QueueCards({ queueStatus, t }) {
    if (!queueStatus) {
        return null
    }

    return (
        <div className="ops-panel p-3">
            <div className="mb-3 flex items-center justify-between gap-3 px-1">
                <div>
                    <p className="ops-kicker">Account Pool</p>
                    <h2 className="ops-heading mt-1">运行队列</h2>
                </div>
                <div className="hidden sm:flex items-center gap-2 text-xs text-muted-foreground">
                    <Gauge className="w-4 h-4 text-primary" />
                    <span>实时并发容量</span>
                </div>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                <QueueMetric
                    icon={CheckCircle2}
                    label={t('accountManager.available')}
                    value={queueStatus.available}
                    unit={t('accountManager.accountsUnit')}
                />
                <QueueMetric
                    icon={Server}
                    label={t('accountManager.inUse')}
                    value={queueStatus.in_use}
                    unit={t('accountManager.threadsUnit')}
                    tone={queueStatus.in_use > 0 ? 'warn' : 'ok'}
                />
                <QueueMetric
                    icon={ShieldCheck}
                    label={t('accountManager.totalPool')}
                    value={queueStatus.total}
                    unit={t('accountManager.accountsUnit')}
                />
            </div>
            {queueStatus.waiting > 0 && (
                <div className="mt-3 flex items-center gap-2 rounded-md border border-amber-300/50 bg-amber-50 px-3 py-2 text-xs font-semibold text-amber-800 page-transition">
                    <Clock3 className="w-4 h-4" />
                    <span>当前有 {queueStatus.waiting} 个请求等待账号释放。</span>
                </div>
            )}
        </div>
    )
}
