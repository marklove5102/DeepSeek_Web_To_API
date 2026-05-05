import { HardDrive } from 'lucide-react'

function updateResponseCache(setForm, patch) {
    setForm((prev) => ({
        ...prev,
        cache: {
            ...prev.cache,
            response: {
                ...prev.cache?.response,
                ...patch,
            },
        },
    }))
}

export default function CacheSection({ t, form, setForm }) {
    const cache = form.cache?.response || {}
    return (
        <div className="bg-card border border-border rounded-xl p-5 space-y-4">
            <div className="flex items-center gap-2">
                <HardDrive className="w-4 h-4 text-muted-foreground" />
                <h3 className="font-semibold">{t('settings.cacheTitle')}</h3>
            </div>
            <p className="text-sm text-muted-foreground">{t('settings.cacheDesc')}</p>
            <label className="flex items-start gap-3 rounded-lg border border-border bg-background/60 p-4">
                <input
                    type="checkbox"
                    checked={Boolean(cache.semantic_key ?? true)}
                    onChange={(e) => updateResponseCache(setForm, { semantic_key: e.target.checked })}
                    className="mt-1 h-4 w-4 rounded border-border"
                />
                <div className="space-y-1">
                    <span className="text-sm font-medium block">{t('settings.cacheSemanticKey')}</span>
                    <span className="text-xs text-muted-foreground block">{t('settings.cacheSemanticKeyDesc')}</span>
                </div>
            </label>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.cacheMemoryTTL')}</span>
                    <input
                        type="number"
                        min={1}
                        max={86400}
                        value={cache.memory_ttl_seconds ?? 300}
                        onChange={(e) => updateResponseCache(setForm, { memory_ttl_seconds: Number(e.target.value || 300) })}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2"
                    />
                </label>
                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.cacheDiskTTL')}</span>
                    <input
                        type="number"
                        min={1}
                        max={604800}
                        value={cache.disk_ttl_seconds ?? 14400}
                        onChange={(e) => updateResponseCache(setForm, { disk_ttl_seconds: Number(e.target.value || 14400) })}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2"
                    />
                </label>
                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.cacheMaxBodyBytes')}</span>
                    <input
                        type="number"
                        min={1}
                        value={cache.max_body_bytes ?? 67108864}
                        onChange={(e) => updateResponseCache(setForm, { max_body_bytes: Number(e.target.value || 67108864) })}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2"
                    />
                </label>
                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.cacheMemoryMaxBytes')}</span>
                    <input
                        type="number"
                        min={1}
                        value={cache.memory_max_bytes ?? 3800000000}
                        onChange={(e) => updateResponseCache(setForm, { memory_max_bytes: Number(e.target.value || 3800000000) })}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2"
                    />
                </label>
                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.cacheDiskMaxBytes')}</span>
                    <input
                        type="number"
                        min={1}
                        value={cache.disk_max_bytes ?? 16000000000}
                        onChange={(e) => updateResponseCache(setForm, { disk_max_bytes: Number(e.target.value || 16000000000) })}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2"
                    />
                </label>
                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.cacheCompression')}</span>
                    <input
                        type="text"
                        value={cache.compression || 'gzip'}
                        readOnly
                        className="w-full bg-muted border border-border rounded-lg px-3 py-2 text-muted-foreground"
                    />
                </label>
                <label className="text-sm space-y-2 md:col-span-3">
                    <span className="text-muted-foreground">{t('settings.cacheDir')}</span>
                    <input
                        type="text"
                        value={cache.dir || ''}
                        readOnly
                        className="w-full bg-muted border border-border rounded-lg px-3 py-2 text-muted-foreground"
                    />
                    <p className="text-xs text-muted-foreground">{t('settings.cacheHotReloadHelp')}</p>
                </label>
            </div>
        </div>
    )
}
