import { useEffect, useState } from "react"
import { authedFetch } from "@/api/client"
import { eventBus } from "@/lib/eventBus"

type Profile = { name: string; displayName: string; overrideCount: number; credentialCount: number }

function getWindowId(): string {
  if (typeof window === "undefined") return "win-1"
  try {
    // Prefer a stable window id threaded in by the desktop shell
    // (?windowId=main). It survives webview reloads, so per-window profile
    // state (active profile, chat config) stays bound to the same desktop
    // window instead of resetting on every reload.
    const fromURL = new URLSearchParams(window.location.search).get("windowId")
    if (fromURL && fromURL.trim()) return fromURL.trim()
    // Fallback: a per-tab random id for the standalone web server, where each
    // browser tab is its own window.
    let id = sessionStorage.getItem("ocode.windowId")
    if (id) return id
    id = `win-${crypto.randomUUID().slice(0, 8)}`
    sessionStorage.setItem("ocode.windowId", id)
    // keep a copy in localStorage for reloads that clear sessionStorage (rare),
    // but don't reuse it for new tabs — each tab gets its own distinct id.
    if (!localStorage.getItem("ocode.windowId")) localStorage.setItem("ocode.windowId", id)
    return id
  } catch {
    return "win-1"
  }
}
const windowId = getWindowId()
export function getActiveWindowId() { return windowId }

export function ProfileSwitcher() {
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [active, setActive] = useState<string>("")
  const [open, setOpen] = useState(false)

  useEffect(() => {
    const refresh = () => {
      authedFetch(`/api/profiles`).then(r=>r.json()).then(d=>setProfiles(d.profiles||[])).catch(()=>{})
      authedFetch(`/api/window/${encodeURIComponent(windowId)}/activeProfile`).then(r=>r.json()).then(d=>setActive(d.activeProfile||"")).catch(()=>{})
    }
    refresh()
    // Re-render the pill whenever the active profile changes for this window
    // (set here or via Settings) or the profile set changes, without a reload.
    const offChanged = eventBus.on("profile.windowChanged", (env) => {
      const data = env.data as { windowId?: string; profile?: string }
      if (!data || !data.windowId || data.windowId === windowId) refresh()
    })
    const offSet = eventBus.on("profile.changed", refresh)
    return () => { offChanged(); offSet() }
  }, [windowId])

  const label = active ? `${active} •${profiles.find(p=>p.name===active)?.overrideCount ?? 0}` : "Base"
  return (
    <div className="relative inline-flex">
      <button
        onClick={()=>setOpen(!open)}
        className="rounded-full border bg-muted px-3 py-1 text-xs hover:bg-accent"
        title={active ? `Profile ${active}` : `Default (base config)`}
      >
        {label} ▾
      </button>
      {open && (
        <div className="absolute right-0 top-8 z-50 w-56 rounded-md border bg-popover p-2 shadow">
          <div className="mb-1 text-xs text-muted-foreground">Active profile for this window</div>
          <button onClick={async()=>{ await authedFetch(`/api/window/${encodeURIComponent(windowId)}/activeProfile`, {method:"PUT", headers:{"Content-Type":"application/json"}, body: JSON.stringify({profile:""})}); setActive(""); setOpen(false)}} className={`flex w-full items-center justify-between rounded px-2 py-1 text-sm hover:bg-accent ${active===""?"bg-accent":""}`}>
            <span>Default (base)</span>{active===""&&<span>✓</span>}
          </button>
          {profiles.map(p=>(
            <button key={p.name} onClick={async()=>{ await authedFetch(`/api/window/${encodeURIComponent(windowId)}/activeProfile`, {method:"PUT", headers:{"Content-Type":"application/json"}, body: JSON.stringify({profile:p.name})}); setActive(p.name); setOpen(false)}} className={`flex w-full items-center justify-between rounded px-2 py-1 text-sm hover:bg-accent ${active===p.name?"bg-accent":""}`}>
              <span>{p.name} •{p.overrideCount}</span>{active===p.name&&<span>✓</span>}
            </button>
          ))}
          <div className="mt-2 border-t pt-2">
            <button onClick={()=>{ setOpen(false); window.dispatchEvent(new CustomEvent("ocode:open-settings-profiles")) }} className="w-full rounded bg-primary px-2 py-1 text-xs text-primary-foreground">Manage profiles…</button>
          </div>
        </div>
      )}
    </div>
  )
}
