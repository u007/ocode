import { useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react"
import { useListNavigation } from "../hooks/useListNavigation"
import { authedFetch } from "@/api/client"
import { eventBus } from "@/lib/eventBus"
import { getWindowId } from "@/lib/windowId"

type Profile = { name: string; displayName: string; overrideCount: number; credentialCount: number }
type ProfileOption = { id: string; profile: string; label: string }

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
  const triggerRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    const refresh = () => {
      authedFetch(`/api/profiles`)
        .then(r => r.json())
        .then(d => setProfiles(d.profiles || []))
        .catch(err => console.error("Failed to load profiles", err))
      authedFetch(`/api/window/${encodeURIComponent(windowId)}/activeProfile`)
        .then(r => r.json())
        .then(d => setActive(d.activeProfile || ""))
        .catch(err => console.error("Failed to load the active profile", err))
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

  const profileOptions: ProfileOption[] = [
    { id: "default", profile: "", label: "Default (base)" },
    ...profiles.map(profile => ({
      id: `profile:${profile.name}`,
      profile: profile.name,
      label: `${profile.name} •${profile.overrideCount}`,
    })),
  ]
  const navigation = useListNavigation({
    itemIds: profileOptions.map(option => option.id),
    initialActiveId: active ? `profile:${active}` : "default",
    resetKey: `${open}|${active}`,
    returnFocusRef: triggerRef,
    onActivate: (index) => {
      const option = profileOptions[index]
      if (option) void handleSelectProfile(option.profile)
    },
    onReturnFocus: () => setOpen(false),
  })

  const handleSelectProfile = async (profile: string) => {
    try {
      await authedFetch(`/api/window/${encodeURIComponent(windowId)}/activeProfile`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ profile }),
      })
      setActive(profile)
      setOpen(false)
      triggerRef.current?.focus({ preventScroll: true })
    } catch (err) {
      console.error("Failed to set the active profile", err)
    }
  }

  const handleListKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Escape") {
      event.preventDefault()
      event.stopPropagation()
      setOpen(false)
      triggerRef.current?.focus({ preventScroll: true })
      return
    }
    if (event.key === "Tab") {
      setOpen(false)
      return
    }
    navigation.onKeyDown(event)
  }

  const label = active ? `${active} •${profiles.find(p => p.name === active)?.overrideCount ?? 0}` : "Base"
  return (
    <div className="relative inline-flex">
      <button
        ref={triggerRef}
        type="button"
        aria-haspopup="true"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="rounded-full border bg-muted px-3 py-1 text-xs hover:bg-accent hover:text-accent-foreground"
        title={active ? `Profile ${active}` : `Default (base config)`}
      >
        {label} ▾
      </button>
      {open && (
        <div
          className="absolute right-0 top-8 z-50 w-56 rounded-md border bg-popover p-2 shadow"
          onKeyDown={handleListKeyDown}
        >
          <div className="mb-1 text-xs text-muted-foreground">Active profile for this window</div>
          {profileOptions.map((option, index) => {
            const selected = active === option.profile
            return (
              <button
                key={option.id}
                {...navigation.getItemProps(index)}
                type="button"
                aria-pressed={selected}
                onClick={() => void handleSelectProfile(option.profile)}
                className={`flex w-full items-center justify-between rounded px-2 py-1 text-sm hover:bg-accent hover:text-accent-foreground ${selected ? "bg-accent text-accent-foreground" : ""} ${navigation.isActive(index) ? "ring-2 ring-inset ring-accent-foreground/60" : ""}`}
              >
                <span>{option.label}</span>{selected && <span>✓</span>}
              </button>
            )
          })}
          <div className="mt-2 border-t pt-2">
            <button
              type="button"
              onClick={() => {
                setOpen(false)
                window.dispatchEvent(new CustomEvent("ocode:open-settings-profiles"))
              }}
              className="w-full rounded bg-primary px-2 py-1 text-xs text-primary-foreground"
            >
              Manage profiles…
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
