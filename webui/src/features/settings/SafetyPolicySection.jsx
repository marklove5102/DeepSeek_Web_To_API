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

export default function SafetyPolicySection({ t, form, setForm }) {
    const safety = form.safety || {}
    const jailbreak = safety.jailbreak || {}
    const enabled = Boolean(safety.enabled)
    const jailbreakEnabled = Boolean(jailbreak.enabled)

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
            </div>
        </div>
    )
}
