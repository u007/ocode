import pathlib

p = pathlib.Path("src/components/Layout/CoworkSidebar.tsx")
s = p.read_text()

# --- 1. imports -------------------------------------------------------------
s = s.replace(
    'import { compactSummaryDisplay, setCompactConfig } from "../../lib/compactConfig";',
    'import { setSpeechSummaryConfig, speechSummaryDisplay } from "../../lib/speechSummaryConfig";',
)

# --- 2. identifiers (all of these are local to this feature) ----------------
renames = [
    ("summaryModel", "speechSummaryModel"),
    ("compactEnabled", "speechSummaryEnabled"),
    ("summaryLoading", "speechSummaryLoading"),
    ("compactOn", "speechSummaryOn"),
    ("toggleSummary", "toggleSpeechSummary"),
    ("compactRes", "speechRes"),
    ("compactSummaryDisplay", "speechSummaryDisplay"),
]
for old, new in renames:
    assert old in s, f"missing {old}"
    s = s.replace(old, new)

# --- 3. the fetch: the compact block -> the speech-summary block ------------
s = s.replace(
    "      api.getCompactConfig(sessionHost).catch(() => null),",
    "      api.getSpeechSummaryConfig(sessionHost).catch(() => null),",
)
s = s.replace(
    "          speechSummaryModel: compactRes?.summary_model || \"\",",
    "          speechSummaryModel: speechRes?.model || \"\",",
)
s = s.replace(
    "          speechSummaryEnabled: typeof compactRes?.enabled === \"boolean\" ? compactRes.enabled : undefined,",
    "          speechSummaryEnabled: typeof speechRes?.enabled === \"boolean\" ? speechRes.enabled : undefined,",
)

# --- 4. the toggle write ----------------------------------------------------
s = s.replace(
    "      const saved = await setCompactConfig({ enabled: next }, sessionHost);",
    "      const saved = await setSpeechSummaryConfig({ enabled: next }, sessionHost);",
)
s = s.replace(
    "        speechSummaryModel: saved?.summary_model ?? prev.speechSummaryModel,",
    "        speechSummaryModel: saved?.model ?? prev.speechSummaryModel,",
)

# --- 5. the dialog purpose + JSX labels -------------------------------------
s = s.replace('onModelClick?.("summary")', 'onModelClick?.("speechsummary")')
s = s.replace(
    '| "autocontinue" | "summary") => void;',
    '| "autocontinue" | "speechsummary") => void;',
)
s = s.replace('<span className="text-muted-foreground">Summary</span>',
              '<span className="text-muted-foreground">Speech summary</span>')
s = s.replace('<span className="text-xs text-muted-foreground">Summary enabled</span>',
              '<span className="text-xs text-muted-foreground">Speech summary enabled</span>')
s = s.replace('aria-label="Summary enabled"', 'aria-label="Speech summary enabled"')
s = s.replace(
    "{speechSummaryDisplay({ summary_model: config.speechSummaryModel })}",
    "{speechSummaryDisplay({ model: config.speechSummaryModel })}",
)

# --- 6. prose: every comment in this block described COMPACTION ------------
prose = [
    (
        "  // Compaction summary row. Unlike every field above, the compact block is a",
        "  // Speech-summary row. Unlike every field above, the speech-summary block is",
    ),
    (
        "  // The compact block has no per-session snapshot, so the persisted config is",
        "  // The speech-summary block has no per-session snapshot, so the persisted",
    ),
    (
        "      // Compaction settings are a persisted global config (no per-session",
        "      // Speech-summary settings are a persisted global config (no per-session",
    ),
    (
        "  // Summary (compaction) on/off: flips compact.enabled — the gate that decides",
        "  // Speech-summary on/off. NOT the compaction gate: this flag decides whether",
    ),
    (
        "  // whether a turn compacts itself at all (internal/agent/agent.go\n"
        "  // resolveCompactRuntime). The model half is the picker above it, mirroring",
        "  // assistant text is shortened by a model before TTS reads it. It is\n"
        "  // deliberately independent of `compact.enabled`.",
    ),
    (
        "  // `enabled` is sent: PUT /api/config/ocode/compact merges the present keys",
        "  // `enabled` is sent: PUT /api/config/ocode/speech-summary merges the present",
    ),
    (
        '      console.error("toggle summary error", e);',
        '      console.error("toggle speech summary error", e);',
    ),
    (
        'reportActionError(e, "Toggling automatic compaction");',
        'reportActionError(e, "Toggling speech summarising");',
    ),
    (
        "          {/* Summary (compaction) — model picker + on/off toggle. Web/desktop",
        "          {/* Speech summary — model picker + on/off toggle. Web/desktop",
    ),
    (
        "              only: the TUI has no summary-model picker, so unlike the rows\n"
        "              above this one has no tuiStatus counterpart and reads the\n"
        "              persisted compact block directly. ●on means automatic compaction\n"
        "              runs at all; the model line is the picker, and picking a model\n"
        "              never flips the gate. */}",
        "              only: the TUI has no speech-summary picker, so unlike the rows\n"
        "              above this one has no tuiStatus counterpart and reads the\n"
        "              persisted speech-summary block directly. ●on means text is\n"
        "              shortened before being spoken; the model line is the picker, and\n"
        "              picking a model never flips the gate. */}",
    ),
    (
        '            title="Pick the compaction summary model"',
        '            title="Pick the model that summarises text before it is spoken"',
    ),
]
for old, new in prose:
    assert s.count(old) == 1, f"prose anchor not found once: {old[:60]!r}"
    s = s.replace(old, new)

p.write_text(s)
print("CoworkSidebar retargeted")
