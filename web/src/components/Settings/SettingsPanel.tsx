import { useState } from "react";
import ModelDefaultsForm from "./ModelDefaultsForm";
import CommitMsgForm from "./CommitMsgForm";

export type SettingsGroupId =
  | "model-defaults"
  | "commit-msg"
  | "compact"
  | "advisor"
  | "permissions"
  | "security"
  | "terminal"
  | "ocr"
  | "discovery"
  | "tui"
  | "editor"
  | "imagegen"
  | "paths"
  | "limits"
  | "features"
  | "plugins"
  | "theme"
  | "opencode-mcp"
  | "opencode-plugins"
  | "opencode-model-state";

interface GroupDef {
  id: SettingsGroupId;
  label: string;
}

const OCODE_GROUPS: GroupDef[] = [
  { id: "model-defaults", label: "Model Defaults & Recap" },
  { id: "commit-msg", label: "Commit Message" },
  { id: "compact", label: "Compact" },
  { id: "advisor", label: "Advisor" },
  { id: "permissions", label: "Permissions" },
  { id: "security", label: "Security & Redaction" },
  { id: "terminal", label: "Terminal" },
  { id: "ocr", label: "OCR" },
  { id: "discovery", label: "Discovery" },
  { id: "tui", label: "TUI" },
  { id: "editor", label: "Editor Mode" },
  { id: "imagegen", label: "Image Generation" },
  { id: "paths", label: "Paths & Uploads" },
  { id: "limits", label: "Limits" },
  { id: "features", label: "Features" },
  { id: "plugins", label: "Plugins & Local Models" },
  { id: "theme", label: "Theme" },
];

const OPENCODE_GROUPS: GroupDef[] = [
  { id: "opencode-mcp", label: "MCP Servers" },
  { id: "opencode-plugins", label: "Legacy Plugins Key" },
  { id: "opencode-model-state", label: "Model Selection State" },
];

// Placeholder content renderer — later tasks replace each case with the
// group's real form component. Kept as a single switch (not a lookup map)
// so each task's diff is a localized one-case addition.
function renderGroup(id: SettingsGroupId) {
  switch (id) {
    case "model-defaults":
      return <ModelDefaultsForm />;
    case "commit-msg":
      return <CommitMsgForm />;
    default:
      return (
        <div className="text-sm text-zinc-500 p-6">
          {OCODE_GROUPS.concat(OPENCODE_GROUPS).find((g) => g.id === id)?.label} — coming soon.
        </div>
      );
  }
}

function NavSection({
  title,
  groups,
  active,
  onSelect,
}: {
  title: string;
  groups: GroupDef[];
  active: SettingsGroupId;
  onSelect: (id: SettingsGroupId) => void;
}) {
  return (
    <div className="mb-4">
      <div className="px-3 py-1.5 text-xs font-semibold uppercase tracking-wide text-zinc-500">
        {title}
      </div>
      {groups.map((g) => (
        <button
          key={g.id}
          type="button"
          onClick={() => onSelect(g.id)}
          className={`w-full text-left px-3 py-1.5 text-sm rounded-md transition-colors ${
            active === g.id
              ? "bg-zinc-700 text-white"
              : "text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200"
          }`}
        >
          {g.label}
        </button>
      ))}
    </div>
  );
}

export default function SettingsPanel() {
  const [active, setActive] = useState<SettingsGroupId>("model-defaults");

  return (
    <div className="flex flex-1 overflow-hidden bg-zinc-900">
      <nav className="w-64 shrink-0 overflow-y-auto border-r border-zinc-700 py-3">
        <NavSection title="ocode" groups={OCODE_GROUPS} active={active} onSelect={setActive} />
        <NavSection title="opencode" groups={OPENCODE_GROUPS} active={active} onSelect={setActive} />
      </nav>
      <div className="flex-1 overflow-y-auto">{renderGroup(active)}</div>
    </div>
  );
}
