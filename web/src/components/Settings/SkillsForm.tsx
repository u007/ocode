import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../../api/client";
import { Loader2 } from "lucide-react";
import type { ManageableSkill } from "../../api/types";

// Global skills settings.
//
// Toggles write to the GLOBAL config layer. The scope is sent EXPLICITLY
// (`scope=global`): omitting the project path is NOT enough, because the
// server's default workDir is never empty in a real deployment, so an
// un-scoped request would write the PROJECT layer of whatever project the
// server last used. A skill switched off here is therefore disabled in every
// NEW chat on this machine. Already-running chats keep their current catalog
// until restarted — this form does not reach into live sessions.
export default function SkillsForm() {
  const [skills, setSkills] = useState<ManageableSkill[]>([]);
  const [loading, setLoading] = useState(true);
  const [skillBusy, setSkillBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  // toggleSeqRef counts the toggles started per skill. A toggle's rollback
  // is only applied if its sequence is still the latest for that skill —
  // otherwise a LATE failure would restore a value that a LATER successful
  // toggle has already replaced.
  const toggleSeqRef = useRef<Record<string, number>>({});

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await api.getManageableSkills(undefined, undefined, true);
      setSkills(res.skills ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const toggleSkill = async (name: string) => {
    const current = skills.find((s) => s.name === name);
    if (!current) return;
    const next = !current.enabled;
    // Claim this skill's sequence number BEFORE the request starts.
    const seq = (toggleSeqRef.current[name] ?? 0) + 1;
    toggleSeqRef.current[name] = seq;
    setSkillBusy(name);
    // Optimistic flip; the refetch below reconciles with the server.
    setSkills((prev) =>
      prev.map((s) => (s.name === name ? { ...s, enabled: next } : s)),
    );
    try {
      await api.setSkillEnabled(name, next, undefined, undefined, undefined, true);
      const fresh = await api.getManageableSkills(undefined, undefined, true);
      setSkills(fresh.skills ?? []);
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      setError(message);
      // Roll back the optimistic flip on failure — but ONLY if this toggle is
      // still the LATEST one for this skill. The value captured at the start of
      // the toggle can be STALE: a later toggle of the same skill may have
      // succeeded in the meantime (the busy guard only blocks that one
      // skill's checkbox, and the finally below clears it unconditionally).
      // Restoring the stale value would then show a state the server does not
      // have, so a superseded rollback is dropped.
      if (toggleSeqRef.current[name] === seq) {
        setSkills((prev) =>
          prev.map((s) =>
            s.name === name && s.enabled === next
              ? { ...s, enabled: current.enabled }
              : s,
          ),
        );
      }
    } finally {
      // Only the latest toggle for this skill may clear the busy flag;
      // a superseded one must not unlock a skill that is still in flight.
      if (toggleSeqRef.current[name] === seq) {
        setSkillBusy(null);
      }
    }
  };

  const orderedSkills = [...skills].sort((a, b) => {
    if (a.enabled !== b.enabled) return a.enabled ? -1 : 1;
    return a.name.localeCompare(b.name);
  });
  const enabledCount = skills.filter((s) => s.enabled).length;

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-muted-foreground animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-2xl space-y-4">
      <div>
        <h2 className="text-sm font-semibold text-foreground">Global Skills</h2>
        <p className="text-xs text-muted-foreground">
          Skills switched off here are disabled in every new chat on this
          machine. Running chats are not changed mid-turn.
        </p>
      </div>

      {error && <div className="text-xs text-red-400">{error}</div>}

      <div className="flex items-center justify-between text-xs text-muted-foreground">
        <span>{enabledCount}/{skills.length} enabled</span>
      </div>

      <div className="border border-border rounded-md">
        {orderedSkills.length === 0 ? (
          <div className="px-4 py-3 text-xs text-muted-foreground">
            No discoverable skills found.
          </div>
        ) : (
          <div className="divide-y divide-border">
            {orderedSkills.map((s) => (
              <label
                key={s.name}
                className="flex items-center justify-between gap-2 cursor-pointer rounded px-3 py-2 hover:bg-muted"
                title={s.description || s.name}
              >
                <div className="flex min-w-0 items-center gap-3">
                  <span
                    className={`font-mono text-[11px] ${s.enabled ? "text-emerald-400" : "text-muted-foreground"}`}
                  >
                    {s.enabled ? "●" : "○"}
                  </span>
                  <div className="min-w-0">
                    <span
                      className={`truncate block font-mono text-xs ${
                        s.enabled ? "text-foreground" : "text-muted-foreground"
                      }`}
                      title={s.name}
                    >
                      {s.name}
                    </span>
                    {s.description && (
                      <span
                        className="truncate block text-[10px] text-muted-foreground"
                        title={s.description}
                      >
                        {s.description}
                      </span>
                    )}
                  </div>
                </div>
                {/* The switch track MUST be `shrink-0`: it is a flex item, and
                    a long, `truncate`d description gives the row a huge base
                    size, so flex would shrink the track below 32px while the
                    absolutely-positioned knob kept its checked offset — the
                    knob then poked out past the track and over the card
                    border, which looked like a broken, half-clipped switch.
                    The off state is a neutral track on purpose: `bg-accent`
                    follows the terminal theme's accent colour, which in many
                    themes is the same green as the on state, so the two
                    states were indistinguishable except by knob position. */}
                <input
                  type="checkbox"
                  aria-label={`Skill ${s.name}`}
                  checked={s.enabled}
                  disabled={skillBusy === s.name}
                  onChange={() => toggleSkill(s.name)}
                  className="w-8 h-4 shrink-0 cursor-pointer rounded-full appearance-none bg-muted-foreground/30 checked:bg-emerald-600 relative before:content-[''] before:absolute before:w-3 before:h-3 before:bg-white before:rounded-full before:top-0.5 before:left-0.5 checked:before:translate-x-4 before:transition-all disabled:opacity-50 disabled:cursor-not-allowed"
                />
              </label>
            ))}
          </div>
        )}
      </div>

      <div className="text-xs text-muted-foreground">
        Changes apply to new chats only. To refresh a running chat, toggle the
        skill from that chat's Cowork sidebar instead.
      </div>
    </div>
  );
}
