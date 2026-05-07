import { ShieldAlert } from 'lucide-react'

function updateSafetyText(setForm, key, value) {
    setForm((prev) => ({
        ...prev,
        safety: {
            ...prev.safety,
            [key]: value,
        },
    }))
}

function updateJailbreak(setForm, patch) {
    setForm((prev) => ({
        ...prev,
        safety: {
            ...prev.safety,
            jailbreak: {
                ...prev.safety?.jailbreak,
                ...patch,
            },
        },
    }))
}

function updateAutoBan(setForm, patch) {
    setForm((prev) => ({
        ...prev,
        safety: {
            ...prev.safety,
            auto_ban: {
                ...prev.safety?.auto_ban,
                ...patch,
            },
        },
    }))
}

function updateLLMCheck(setForm, patch) {
    setForm((prev) => ({
        ...prev,
        safety: {
            ...prev.safety,
            llm_check: {
                ...prev.safety?.llm_check,
                ...patch,
            },
        },
    }))
}

export default function SafetyPolicySection({ t, form, setForm }) {
    const safety = form.safety || {}
    const jailbreak = safety.jailbreak || {}
    const autoBan = safety.auto_ban || {}
    const llmCheck = safety.llm_check || {}
    const enabled = Boolean(safety.enabled)
    const jailbreakEnabled = Boolean(jailbreak.enabled)
    const autoBanEnabled = autoBan.enabled !== false
    const llmCheckEnabled = Boolean(llmCheck.enabled)
    const llmCheckFailOpen = llmCheck.fail_open !== false

    return (
        <div className="bg-card border border-border rounded-xl p-5 space-y-4">
            <div className="flex items-center gap-2">
                <ShieldAlert className="w-4 h-4 text-muted-foreground" />
                <h3 className="font-semibold">{t('settings.safetyPolicyTitle')}</h3>
            </div>
            <p className="text-sm text-muted-foreground">{t('settings.safetyPolicyDesc')}</p>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <label className="flex items-start gap-3 rounded-lg border border-border bg-background/60 p-4">
                    <input
                        type="checkbox"
                        checked={enabled}
                        onChange={(e) => setForm((prev) => ({
                            ...prev,
                            safety: {
                                ...prev.safety,
                                enabled: e.target.checked,
                            },
                        }))}
                        className="mt-1 h-4 w-4 rounded border-border"
                    />
                    <div className="space-y-1">
                        <span className="text-sm font-medium block">{t('settings.safetyEnabled')}</span>
                        <span className="text-xs text-muted-foreground block">{t('settings.safetyEnabledDesc')}</span>
                    </div>
                </label>

                <label className="flex items-start gap-3 rounded-lg border border-border bg-background/60 p-4">
                    <input
                        type="checkbox"
                        checked={jailbreakEnabled}
                        onChange={(e) => updateJailbreak(setForm, { enabled: e.target.checked })}
                        className="mt-1 h-4 w-4 rounded border-border"
                    />
                    <div className="space-y-1">
                        <span className="text-sm font-medium block">{t('settings.jailbreakEnabled')}</span>
                        <span className="text-xs text-muted-foreground block">{t('settings.jailbreakEnabledDesc')}</span>
                    </div>
                </label>

                <label className="text-sm space-y-2 md:col-span-2">
                    <span className="text-muted-foreground">{t('settings.blockMessage')}</span>
                    <input
                        type="text"
                        value={safety.block_message || ''}
                        onChange={(e) => updateSafetyText(setForm, 'block_message', e.target.value)}
                        placeholder={t('settings.blockMessagePlaceholder')}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2"
                    />
                </label>

                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.blockedIps')}</span>
                    <textarea
                        rows={5}
                        value={safety.blocked_ips_text || ''}
                        onChange={(e) => updateSafetyText(setForm, 'blocked_ips_text', e.target.value)}
                        placeholder={t('settings.blockedIpsPlaceholder')}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2 resize-y min-h-32"
                    />
                </label>

                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.allowedIps')}</span>
                    <textarea
                        rows={5}
                        value={safety.allowed_ips_text || ''}
                        onChange={(e) => updateSafetyText(setForm, 'allowed_ips_text', e.target.value)}
                        placeholder={t('settings.allowedIpsPlaceholder')}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2 resize-y min-h-32"
                    />
                </label>

                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.blockedConversationIds')}</span>
                    <textarea
                        rows={5}
                        value={safety.blocked_conversation_ids_text || ''}
                        onChange={(e) => updateSafetyText(setForm, 'blocked_conversation_ids_text', e.target.value)}
                        placeholder={t('settings.blockedConversationIdsPlaceholder')}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2 resize-y min-h-32"
                    />
                </label>

                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.bannedContent')}</span>
                    <textarea
                        rows={5}
                        value={safety.banned_content_text || ''}
                        onChange={(e) => updateSafetyText(setForm, 'banned_content_text', e.target.value)}
                        placeholder={t('settings.bannedContentPlaceholder')}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2 resize-y min-h-32"
                    />
                </label>

                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.bannedRegex')}</span>
                    <textarea
                        rows={5}
                        value={safety.banned_regex_text || ''}
                        onChange={(e) => updateSafetyText(setForm, 'banned_regex_text', e.target.value)}
                        placeholder={t('settings.bannedRegexPlaceholder')}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2 resize-y min-h-32"
                    />
                </label>

                <label className="text-sm space-y-2 md:col-span-2">
                    <span className="text-muted-foreground">{t('settings.jailbreakPatterns')}</span>
                    <textarea
                        rows={5}
                        value={jailbreak.patterns_text || ''}
                        onChange={(e) => updateJailbreak(setForm, { patterns_text: e.target.value })}
                        placeholder={t('settings.jailbreakPatternsPlaceholder')}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2 resize-y min-h-32"
                    />
                    <p className="text-xs text-muted-foreground">{t('settings.safetyPolicyHelp')}</p>
                </label>

                <div className="md:col-span-2 rounded-lg border border-border bg-background/60 p-4 space-y-3">
                    <label className="flex items-start gap-3">
                        <input
                            type="checkbox"
                            checked={autoBanEnabled}
                            onChange={(e) => updateAutoBan(setForm, { enabled: e.target.checked })}
                            className="mt-1 h-4 w-4 rounded border-border"
                        />
                        <div className="space-y-1">
                            <span className="text-sm font-medium block">{t('settings.autoBanEnabled')}</span>
                            <span className="text-xs text-muted-foreground block">{t('settings.autoBanEnabledDesc')}</span>
                        </div>
                    </label>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                        <label className="text-sm space-y-1">
                            <span className="text-muted-foreground">{t('settings.autoBanThreshold')}</span>
                            <input
                                type="number"
                                min={1}
                                max={1000000}
                                value={autoBan.threshold ?? 3}
                                onChange={(e) => {
                                    const n = Number(e.target.value)
                                    updateAutoBan(setForm, { threshold: Number.isFinite(n) && n >= 1 ? Math.min(n, 1000000) : 3 })
                                }}
                                disabled={!autoBanEnabled}
                                className="w-full bg-background border border-border rounded-lg px-3 py-2 disabled:opacity-50"
                            />
                        </label>
                        <label className="text-sm space-y-1">
                            <span className="text-muted-foreground">{t('settings.autoBanWindowSeconds')}</span>
                            <input
                                type="number"
                                min={1}
                                max={2592000}
                                value={autoBan.window_seconds ?? 600}
                                onChange={(e) => {
                                    const n = Number(e.target.value)
                                    updateAutoBan(setForm, { window_seconds: Number.isFinite(n) && n >= 1 ? Math.min(n, 2592000) : 600 })
                                }}
                                disabled={!autoBanEnabled}
                                className="w-full bg-background border border-border rounded-lg px-3 py-2 disabled:opacity-50"
                            />
                        </label>
                    </div>
                </div>

                <div className="md:col-span-2 rounded-lg border border-border bg-background/60 p-4 space-y-3">
                    <label className="flex items-start gap-3">
                        <input
                            type="checkbox"
                            checked={llmCheckEnabled}
                            onChange={(e) => updateLLMCheck(setForm, { enabled: e.target.checked })}
                            className="mt-1 h-4 w-4 rounded border-border"
                        />
                        <div className="space-y-1">
                            <span className="text-sm font-medium block">{t('settings.llmCheckEnabled')}</span>
                            <span className="text-xs text-muted-foreground block">{t('settings.llmCheckEnabledDesc')}</span>
                        </div>
                    </label>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                        <label className="text-sm space-y-1 md:col-span-2">
                            <span className="text-muted-foreground">{t('settings.llmCheckModel')}</span>
                            <input
                                type="text"
                                value={llmCheck.model || ''}
                                onChange={(e) => updateLLMCheck(setForm, { model: e.target.value })}
                                placeholder="deepseek-v4-flash-nothinking"
                                disabled={!llmCheckEnabled}
                                className="w-full bg-background border border-border rounded-lg px-3 py-2 disabled:opacity-50"
                            />
                        </label>
                        <label className="text-sm space-y-1">
                            <span className="text-muted-foreground">{t('settings.llmCheckTimeoutMs')}</span>
                            <input
                                type="number"
                                min={100}
                                max={60000}
                                value={llmCheck.timeout_ms ?? 5000}
                                onChange={(e) => updateLLMCheck(setForm, { timeout_ms: Number(e.target.value) || 5000 })}
                                disabled={!llmCheckEnabled}
                                className="w-full bg-background border border-border rounded-lg px-3 py-2 disabled:opacity-50"
                            />
                        </label>
                        <label className="flex items-start gap-3">
                            <input
                                type="checkbox"
                                checked={llmCheckFailOpen}
                                onChange={(e) => updateLLMCheck(setForm, { fail_open: e.target.checked })}
                                disabled={!llmCheckEnabled}
                                className="mt-1 h-4 w-4 rounded border-border disabled:opacity-50"
                            />
                            <div className="space-y-1">
                                <span className="text-sm font-medium block">{t('settings.llmCheckFailOpen')}</span>
                                <span className="text-xs text-muted-foreground block">{t('settings.llmCheckFailOpenDesc')}</span>
                            </div>
                        </label>
                        <label className="text-sm space-y-1">
                            <span className="text-muted-foreground">{t('settings.llmCheckMinInputChars')}</span>
                            <input
                                type="number"
                                min={0}
                                max={1000000}
                                value={llmCheck.min_input_chars ?? 30}
                                onChange={(e) => updateLLMCheck(setForm, { min_input_chars: Number(e.target.value) || 0 })}
                                disabled={!llmCheckEnabled}
                                className="w-full bg-background border border-border rounded-lg px-3 py-2 disabled:opacity-50"
                            />
                        </label>
                        <label className="text-sm space-y-1">
                            <span className="text-muted-foreground">{t('settings.llmCheckMaxInputChars')}</span>
                            <input
                                type="number"
                                min={1}
                                max={1000000}
                                value={llmCheck.max_input_chars ?? 8000}
                                onChange={(e) => updateLLMCheck(setForm, { max_input_chars: Number(e.target.value) || 8000 })}
                                disabled={!llmCheckEnabled}
                                className="w-full bg-background border border-border rounded-lg px-3 py-2 disabled:opacity-50"
                            />
                        </label>
                        <label className="text-sm space-y-1">
                            <span className="text-muted-foreground">{t('settings.llmCheckCacheTTL')}</span>
                            <input
                                type="number"
                                min={1}
                                max={604800}
                                value={llmCheck.cache_ttl_seconds ?? 600}
                                onChange={(e) => updateLLMCheck(setForm, { cache_ttl_seconds: Number(e.target.value) || 600 })}
                                disabled={!llmCheckEnabled}
                                className="w-full bg-background border border-border rounded-lg px-3 py-2 disabled:opacity-50"
                            />
                        </label>
                        <label className="text-sm space-y-1">
                            <span className="text-muted-foreground">{t('settings.llmCheckMaxConcurrent')}</span>
                            <input
                                type="number"
                                min={1}
                                max={1024}
                                value={llmCheck.max_concurrent ?? 16}
                                onChange={(e) => updateLLMCheck(setForm, { max_concurrent: Number(e.target.value) || 16 })}
                                disabled={!llmCheckEnabled}
                                className="w-full bg-background border border-border rounded-lg px-3 py-2 disabled:opacity-50"
                            />
                        </label>
                        <label className="text-sm space-y-1">
                            <span className="text-muted-foreground">{t('settings.llmCheckCacheMax')}</span>
                            <input
                                type="number"
                                min={1}
                                max={1000000}
                                value={llmCheck.cache_max_entries ?? 10000}
                                onChange={(e) => updateLLMCheck(setForm, { cache_max_entries: Number(e.target.value) || 10000 })}
                                disabled={!llmCheckEnabled}
                                className="w-full bg-background border border-border rounded-lg px-3 py-2 disabled:opacity-50"
                            />
                        </label>
                    </div>
                    <p className="text-xs text-muted-foreground">{t('settings.llmCheckHelp')}</p>
                </div>
            </div>
        </div>
    )
}
