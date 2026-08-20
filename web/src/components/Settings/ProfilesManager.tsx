import { useEffect, useState } from "react"
import { getActiveWindowId } from "../ProfileSwitcher"

type Profile = { name: string; displayName: string; overrideCount: number; credentialCount: number }
type Cred = { provider: string; label: string; masked: string; kind: string }

const PROVIDERS = ["openai","anthropic","google","opencode","openrouter","zai","deepseek","alibaba","minimax","moonshot","copilot","orcarouter","chutes","deepinfra","nvidia","alibaba-coding","zai-coding","requesty","grok","novita-ai","lmstudio","cloudflare-workers","cloudflare-gateway","codex","opencode-go"] as const

export default function ProfilesManager() {
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [active, setActive] = useState<string>("")
  const [loading, setLoading] = useState(true)
  const [newName, setNewName] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [expanded, setExpanded] = useState<string | null>(null)
  const [eff, setEff] = useState<any>(null)
  const [creds, setCreds] = useState<Cred[]>([])
  const [credProvider, setCredProvider] = useState<string>("openai")
  const [credKey, setCredKey] = useState("")
  const windowId = getActiveWindowId()

  const refresh = async () => {
    setLoading(true)
    try {
      const [pRes, wRes] = await Promise.all([
        fetch("/api/profiles").then(r=>r.json()),
        fetch(`/api/window/${encodeURIComponent(windowId)}/activeProfile`).then(r=>r.json()).catch(()=>({activeProfile:""})),
      ])
      setProfiles(pRes.profiles || [])
      setActive(wRes.activeProfile || wRes.effectiveProfile || "")
    } catch (e) {
      setError(String(e))
    } finally { setLoading(false) }
  }
  useEffect(()=>{ refresh() }, [])

  const loadDetail = async (name: string) => {
    if (expanded === name) { setExpanded(null); return }
    setExpanded(name)
    setEff(null); setCreds([])
    try {
      const [eRes, cRes] = await Promise.all([
        fetch(`/api/profiles/${encodeURIComponent(name)}/effective`).then(r=>r.json()),
        fetch(`/api/profiles/${encodeURIComponent(name)}/auth`).then(r=>r.json()),
      ])
      setEff(eRes)
      setCreds(cRes.credentials || [])
    } catch {}
  }

  const create = async () => {
    const name = newName.trim().toLowerCase()
    if (!name) return
    if (!/^[a-z0-9_-]{1,32}$/.test(name)) { setError("name must match [a-z0-9_-]{1,32}"); return }
    setError(null)
    const res = await fetch("/api/profiles", { method:"POST", headers:{"Content-Type":"application/json"}, body: JSON.stringify({ name }) })
    if (!res.ok) { const t = await res.text(); setError(t); return }
    setNewName("")
    refresh()
  }
  const rename = async (oldName: string) => {
    const n = prompt(`Rename "${oldName}" to:`, oldName)
    if (!n || n===oldName) return
    const nn = n.trim().toLowerCase()
    if (!/^[a-z0-9_-]{1,32}$/.test(nn)) { setError("name must match [a-z0-9_-]{1,32}"); return }
    const res = await fetch(`/api/profiles/${encodeURIComponent(oldName)}/rename`, { method:"POST", headers:{"Content-Type":"application/json"}, body: JSON.stringify({newName: nn})})
    if (!res.ok) { const t=await res.text(); setError(t); return }
    refresh()
  }
  const remove = async (name: string, credCount:number, overrideCount:number) => {
    if (active === name) { setError(`Cannot delete "${name}" — it is active in this window. Switch to Default first.`); return }
    const ok = confirm(`Delete "${name}"? Removes ${overrideCount} overrides + ${credCount} keys — cannot undo.`)
    if (!ok) return
    const res = await fetch(`/api/profiles/${encodeURIComponent(name)}`, { method:"DELETE" })
    if (!res.ok) { const t=await res.text(); setError(t.includes("active") ? `Cannot delete "${name}" — switch to Default first.` : t); return }
    refresh()
  }
  const setActiveProfile = async (name:string) => {
    const res = await fetch(`/api/window/${encodeURIComponent(windowId)}/activeProfile`, { method:"PUT", headers:{"Content-Type":"application/json"}, body: JSON.stringify({profile: name})})
    if (!res.ok) { const t=await res.text(); setError(t); return }
    setActive(name)
  }
  const saveKey = async (name: string) => {
    if (!credKey.trim()) { setError("apiKey required"); return }
    const res = await fetch(`/api/profiles/${encodeURIComponent(name)}/auth/${encodeURIComponent(credProvider)}`, { method:"PUT", headers:{"Content-Type":"application/json"}, body: JSON.stringify({apiKey: credKey.trim()})})
    if (!res.ok) { const t=await res.text(); setError(t); return }
    setCredKey("")
    const cRes = await fetch(`/api/profiles/${encodeURIComponent(name)}/auth`).then(r=>r.json())
    setCreds(cRes.credentials || [])
    refresh()
  }
  const deleteKey = async (name: string, provider: string) => {
    if (!confirm(`Remove ${provider} key from "${name}"?`)) return
    const res = await fetch(`/api/profiles/${encodeURIComponent(name)}/auth/${encodeURIComponent(provider)}`, { method:"DELETE" })
    if (!res.ok) { const t=await res.text(); setError(t); return }
    const cRes = await fetch(`/api/profiles/${encodeURIComponent(name)}/auth`).then(r=>r.json())
    setCreds(cRes.credentials || [])
    refresh()
  }
  const resetField = async (name: string, field: string) => {
    const res = await fetch(`/api/profiles/${encodeURIComponent(name)}/overrides/${encodeURIComponent(field)}`, { method:"DELETE" })
    if (!res.ok) { const t=await res.text(); setError(t); return }
    const eRes = await fetch(`/api/profiles/${encodeURIComponent(name)}/effective`).then(r=>r.json())
    setEff(eRes)
    refresh()
  }

  if (loading) return <div className="p-6 text-sm text-zinc-400">Loading profiles…</div>
  return (
    <div className="p-6 space-y-6 max-w-3xl">
      <div>
        <h2 className="text-lg font-semibold">Profiles</h2>
        <p className="text-sm text-zinc-400">Sparse overlays over the base config. Default = base alone. Per-window active profile hot-swaps on next turn. Sidecar <code className="bg-zinc-800 px-1 rounded">auth.profiles.json</code> keeps opencode compat. Alias: <code className="bg-zinc-800 px-1 rounded">OCODE_PROFILE=work ocode</code> or <code className="bg-zinc-800 px-1 rounded">ocode --profile work</code></p>
      </div>

      {error && <div className="rounded bg-red-900/30 border border-red-700 p-3 text-sm text-red-300">{error}</div>}

      <div className="flex gap-2">
        <input value={newName} onChange={e=>setNewName(e.target.value)} placeholder="new profile name (a-z0-9_-)" className="flex-1 rounded bg-zinc-800 border border-zinc-700 px-3 py-2 text-sm" />
        <button onClick={create} className="rounded bg-white text-black px-4 py-2 text-sm font-medium hover:bg-zinc-200">+ New</button>
        <button onClick={refresh} className="rounded border border-zinc-700 px-3 py-2 text-sm">Refresh</button>
      </div>

      <div className="space-y-2">
        <div className={`flex items-center justify-between rounded border p-3 ${active===""?"border-white bg-zinc-800":"border-zinc-700 bg-zinc-800/50"}`}>
          <div>
            <div className="font-medium text-sm">Default (base)</div>
            <div className="text-xs text-zinc-400">No profile — inherits base config only</div>
          </div>
          <button onClick={()=>setActiveProfile("")} className={`rounded px-3 py-1 text-xs ${active==="" ? "bg-white text-black":"border border-zinc-600 hover:bg-zinc-700"}`}>{active===""?"✓ Active":"Activate"}</button>
        </div>
        {profiles.map(p=>(
          <div key={p.name} className={`rounded border ${active===p.name?"border-white bg-zinc-800":"border-zinc-700 bg-zinc-800/50"}`}>
            <div className="flex items-center justify-between p-3">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <button onClick={()=>loadDetail(p.name)} className="font-mono text-sm font-medium hover:underline">{p.name} {expanded===p.name?"▾":"▸"}</button>
                  {p.overrideCount>0 ? <span className="text-xs text-amber-300">● overridden •{p.overrideCount}</span> : <span className="text-xs text-zinc-500">inherited</span>}
                  {p.credentialCount>0 ? <span className="text-xs text-zinc-400">keys •••• ({p.credentialCount})</span> : null}
                  {active===p.name && <span className="text-xs bg-white text-black px-2 py-0.5 rounded">active</span>}
                </div>
                {p.displayName && <div className="text-xs text-zinc-400">{p.displayName}</div>}
              </div>
              <div className="flex gap-1 shrink-0">
                <button onClick={()=>setActiveProfile(p.name)} className={`rounded px-2 py-1 text-xs ${active===p.name?"bg-white text-black":"border border-zinc-600 hover:bg-zinc-700"}`}>{active===p.name?"✓":"Activate"}</button>
                <button onClick={()=>rename(p.name)} className="rounded border border-zinc-600 px-2 py-1 text-xs hover:bg-zinc-700">Rename</button>
                <button onClick={()=>remove(p.name, p.credentialCount, p.overrideCount)} className="rounded border border-red-700 text-red-300 px-2 py-1 text-xs hover:bg-red-900/30">Delete</button>
              </div>
            </div>
            {expanded===p.name && (
              <div className="border-t border-zinc-700 p-3 space-y-4 bg-zinc-900/30">
                {/* Credentials */}
                <div>
                  <div className="text-xs font-semibold uppercase tracking-wide text-zinc-400 mb-2">API keys for this profile (stored in auth.profiles.json, never in auth.json)</div>
                  {creds.length===0 ? <div className="text-xs text-zinc-500 mb-2">No keys yet — add one below. Unset providers inherit base auth.json / env.</div> : (
                    <div className="space-y-1 mb-2">
                      {creds.map(c=>(
                        <div key={c.provider} className="flex items-center justify-between rounded bg-zinc-800 px-2 py-1 text-xs">
                          <span className="font-mono">{c.provider}</span>
                          <span className="text-zinc-400">{c.masked} ({c.kind})</span>
                          <button onClick={()=>deleteKey(p.name, c.provider)} className="rounded border border-red-700 px-2 py-0.5 text-red-300 hover:bg-red-900/30">Remove</button>
                        </div>
                      ))}
                    </div>
                  )}
                  <div className="flex gap-2">
                    <select value={credProvider} onChange={e=>setCredProvider(e.target.value)} className="rounded bg-zinc-800 border border-zinc-700 px-2 py-1 text-xs">
                      {PROVIDERS.map(id=> <option key={id} value={id}>{id}</option>)}
                    </select>
                    <input value={credKey} onChange={e=>setCredKey(e.target.value)} placeholder="sk-... (apiKey)" className="flex-1 rounded bg-zinc-800 border border-zinc-700 px-2 py-1 text-xs" type="password" />
                    <button onClick={()=>saveKey(p.name)} className="rounded bg-white text-black px-3 py-1 text-xs">Save key</button>
                  </div>
                </div>
                {/* Effective diff */}
                {eff && (
                  <div>
                    <div className="text-xs font-semibold uppercase tracking-wide text-zinc-400 mb-2">Overrides (sparse) — click Reset to inherit base</div>
                    {eff.delta && Object.keys(eff.delta).filter(k=>eff.delta[k]!=null && eff.delta[k]!==undefined && (Array.isArray(eff.delta[k]) ? eff.delta[k].length>0 : typeof eff.delta[k]==="object" ? Object.keys(eff.delta[k]).length>0 : true)).length===0 ? <div className="text-xs text-zinc-500">No overrides — fully inherited.</div> : (
                      <div className="space-y-1">
                        {Object.entries(eff.delta || {}).map(([k,v])=>{
                          if (v==null) return null
                          if (typeof v==="object" && !Array.isArray(v) && Object.keys(v as any).length===0) return null
                          // provider map: show per-provider rows
                          if (k==="provider" && v && typeof v==="object") {
                            return Object.keys(v as any).map(pid=>(
                              <div key={`provider.${pid}`} className="flex items-center justify-between rounded bg-zinc-800 px-2 py-1 text-xs">
                                <span className="font-mono">provider.{pid}</span>
                                <span className="text-amber-300">● overridden</span>
                                <button onClick={()=>resetField(p.name, `provider.${pid}`)} className="rounded border border-zinc-600 px-2 py-0.5 hover:bg-zinc-700">Reset to base</button>
                              </div>
                            ))
                          }
                          if (k==="mcp" && v && typeof v==="object") {
                            return Object.keys(v as any).map(mid=>(
                              <div key={`mcp.${mid}`} className="flex items-center justify-between rounded bg-zinc-800 px-2 py-1 text-xs">
                                <span className="font-mono">mcp.{mid}</span>
                                <span className="text-amber-300">● overridden</span>
                                <button onClick={()=>resetField(p.name, `mcp.${mid}`)} className="rounded border border-zinc-600 px-2 py-0.5 hover:bg-zinc-700">Reset to base</button>
                              </div>
                            ))
                          }
                          return (
                            <div key={k} className="flex items-center justify-between rounded bg-zinc-800 px-2 py-1 text-xs">
                              <span className="font-mono">{k}</span>
                              <span className="text-amber-300">● overridden</span>
                              <button onClick={()=>resetField(p.name, k)} className="rounded border border-zinc-600 px-2 py-0.5 hover:bg-zinc-700">Reset to base</button>
                            </div>
                          )
                        })}
                      </div>
                    )}
                    <div className="mt-2 text-xs text-zinc-500">Effective preview: model=<span className="font-mono text-zinc-300">{eff.effective?.model || "(base)"}</span> — full JSON via <code>GET /api/profiles/{p.name}/effective</code></div>
                  </div>
                )}
              </div>
            )}
          </div>
        ))}
        {profiles.length===0 && <div className="text-sm text-zinc-500">No profiles yet — create one above. Overrides stay sparse: unset fields inherit base.</div>}
      </div>

      <div className="rounded border border-zinc-700 bg-zinc-800/30 p-4 text-xs text-zinc-400 space-y-1">
        <div className="font-medium text-zinc-300">Behavior</div>
        <ul className="list-disc pl-5 space-y-1">
          <li>Switching profile hot-swaps on next turn (mid-stream turn finishes on old profile).</li>
          <li>Reset to base = delete field from profile delta (per-field button above; whole profile delete = <code>DELETE /api/profiles/{`{name}`}</code>).</li>
          <li>Delete blocked while active — server returns 409; switch to Default first.</li>
          <li>Env <code>OCODE_PROFILE</code> wins over window state for <code>alias ocode2='OCODE_PROFILE=work ocode'</code>.</li>
        </ul>
      </div>
    </div>
  )
}
