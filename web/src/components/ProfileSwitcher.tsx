import { useEffect, useState } from "react"
import { authedFetch } from "@/api/client"
import { eventBus } from "@/lib/eventBus"
import { getWindowId } from "@/lib/windowId"

type Profile = { name: string; displayName: string; overrideCount: number; credentialCount: number }

// Resolved once per app load through the shared helper so the profile pill and
// every chat request agree on the window. The helper persists a URL-derived id
// to sessionStorage, so a later SPA navigation (the desktop deep-link redirect)
// that drops ?windowId= still resolves the same window instead of minting a
// fresh random one — the divergence that made a picked profile invisible to an
// already-open chat.
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
        className="rounded-full border bg-muted px-3 py-1 text-xs hover:bg-accent hover:text-accent-foreground"
        title={active ? `Profile ${active}` : `Default (base config)`}
      >
        {label} ▾
      </button>
      {open && (
        <div className="absolute right-0 top-8 z-50 w-56 rounded-md border bg-popover p-2 shadow">
          <div className="mb-1 text-xs text-muted-foreground">Active profile for this window</div>
          <button onClick={async()=>{ await authedFetch(`/api/window/${encodeURIComponent(windowId)}/activeProfile`, {method:"PUT", headers:{"Content-Type":"application/json"}, body: JSON.stringify({profile:""})}); setActive(""); setOpen(false)}} className={`flex w-full items-center justify-between rounded px-2 py-1 text-sm hover:bg-accent hover:text-accent-foreground ${active===""?"bg-accent text-accent-foreground":""}`}>
            <span>Default (base)</span>{active===""&&<span>✓</span>}
          </button>
          {profiles.map(p=>(
            <button key={p.name} onClick={async()=>{ await authedFetch(`/api/window/${encodeURIComponent(windowId)}/activeProfile`, {method:"PUT", headers:{"Content-Type":"application/json"}, body: JSON.stringify({profile:p.name})}); setActive(p.name); setOpen(false)}} className={`flex w-full items-center justify-between rounded px-2 py-1 text-sm hover:bg-accent hover:text-accent-foreground ${active===p.name?"bg-accent text-accent-foreground":""}`}>
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
