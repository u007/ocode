import { useCallback, useEffect, useState } from "react";
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  rectSortingStrategy,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GripVertical, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  useQuickActions,
  saveQuickActions,
  mintQuickActionId,
  QUICK_ACTION_ICONS,
  QUICK_ACTIONS_MAX,
  quickActionIconComponent,
} from "@/lib/quickActions";
import type { QuickActionChip, QuickActionMode } from "@/api/types";

/**
 * The mode options, in the order the composer interprets them. Rendered from a
 * literal list rather than derived from a type so adding a third mode is a
 * deliberate edit here instead of a silent runtime union.
 */
const MODES: Array<{ value: QuickActionMode; label: string }> = [
  { value: "fill", label: "Fill the composer" },
  { value: "send", label: "Send immediately" },
];

const selectClass =
  "rounded border border-border bg-background px-2 py-1 text-xs text-foreground";

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

interface ChipRowProps {
  chip: QuickActionChip;
  index: number;
  onChange: (index: number, patch: Partial<QuickActionChip>) => void;
  onDelete: (index: number) => void;
}

function ChipRow({ chip, index, onChange, onDelete }: ChipRowProps) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: chip.id,
  });
  const style = { transform: CSS.Transform.toString(transform), transition, opacity: isDragging ? 0.5 : 1 };
  const RowIcon = quickActionIconComponent(chip.icon);
  const n = index + 1;

  return (
    <li ref={setNodeRef} style={style} className="rounded border border-border p-2">
      {/* The drag handle, not the row, carries the dnd-kit listeners: the row
          holds real buttons, and dnd-kit's `attributes` set role="button",
          which would nest one interactive role inside another. */}
      <div className="flex items-center gap-2">
        <button
          type="button"
          aria-label={`Reorder chip ${n}`}
          className="shrink-0 cursor-grab rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
          {...attributes}
          {...listeners}
        >
          <GripVertical className="h-4 w-4" aria-hidden="true" />
        </button>
        <RowIcon className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />

        <Input
          aria-label={`Chip label for chip ${n}`}
          value={chip.label}
          onChange={(event) => onChange(index, { label: event.target.value })}
          className="h-8 flex-1 text-xs"
        />

        <select
          aria-label={`Chip icon for chip ${n}`}
          className={selectClass}
          value={QUICK_ACTION_ICONS.includes(chip.icon as (typeof QUICK_ACTION_ICONS)[number]) ? chip.icon : "zap"}
          onChange={(event) => onChange(index, { icon: event.target.value })}
        >
          {QUICK_ACTION_ICONS.map((key) => {
            const Icon = quickActionIconComponent(key);
            return (
              <option key={key} value={key}>
                <Icon className="h-3 w-3" aria-hidden="true" /> {key}
              </option>
            );
          })}
        </select>

        <select
          aria-label={`Chip mode for chip ${n}`}
          className={selectClass}
          value={chip.mode}
          onChange={(event) => onChange(index, { mode: event.target.value as QuickActionMode })}
        >
          {MODES.map((mode) => (
            <option key={mode.value} value={mode.value}>
              {mode.label}
            </option>
          ))}
        </select>

        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="h-8 w-8 shrink-0"
          aria-label={`Delete chip ${n}`}
          onClick={() => onDelete(index)}
        >
          <Trash2 className="h-4 w-4" aria-hidden="true" />
        </Button>
      </div>

      <Input
        aria-label={`Chip message for chip ${n}`}
        value={chip.message}
        onChange={(event) => onChange(index, { message: event.target.value })}
        placeholder="Message sent when the chip is clicked"
        className="mt-2 h-8 text-xs"
      />
    </li>
  );
}

export default function QuickActionsForm() {
  const { chips, loading, error: loadError, revision } = useQuickActions();
  const [draft, setDraft] = useState<QuickActionChip[]>(chips);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  // The shared store is the single source of truth: rebase the draft whenever a
  // fetch, another client's save, or our own successful save publishes a new
  // revision. Deliberately keyed on `revision` and NOT on `chips`: a re-publish
  // of an identical list hands over a fresh array, and depending on that would
  // silently wipe an edit in progress. `revision` is the store's documented
  // "a rendered field actually changed" key.
  useEffect(() => {
    setDraft(chips);
  }, [revision]);

  const dndSensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const handleDragEnd = useCallback((event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    setDraft((prev) => {
      const oldIndex = prev.findIndex((c) => c.id === active.id);
      const newIndex = prev.findIndex((c) => c.id === over.id);
      if (oldIndex === -1 || newIndex === -1) return prev;
      return arrayMove(prev, oldIndex, newIndex);
    });
  }, []);

  const updateChip = useCallback((index: number, patch: Partial<QuickActionChip>) => {
    setDraft((prev) => prev.map((chip, i) => (i === index ? { ...chip, ...patch } : chip)));
    setSaveError(null);
  }, []);

  const deleteChip = useCallback((index: number) => {
    setDraft((prev) => prev.filter((_, i) => i !== index));
    setSaveError(null);
  }, []);

  // Review Focus #2: the minted id must never collide with a reserved starter
  // slug or an id already in the draft, or two rows share a React key and one
  // silently vanishes.
  const addChip = useCallback(() => {
    setDraft((prev) => {
      if (prev.length >= QUICK_ACTIONS_MAX) return prev;
      const id = mintQuickActionId(prev);
      return [...prev, { id, label: "New chip", icon: "zap", message: "", mode: "fill" }];
    });
    setSaveError(null);
  }, []);

  /**
   * The server is the validation authority, so its message is surfaced
   * VERBATIM and the draft is left exactly as the user left it (Review Focus
   * #1 and #4). No local copy of the rules: a second implementation of the cap
   * and the whitespace checks is the duplication class this spec avoids, and it
   * would swallow exactly the message the user needs.
   */
  const onSave = useCallback(async () => {
    setSaving(true);
    setSaveError(null);
    try {
      // On success the store publishes the server's answer, which changes
      // `revision` and rebases the draft onto what was actually persisted.
      await saveQuickActions(draft);
    } catch (error) {
      setSaveError(errorText(error));
    } finally {
      setSaving(false);
    }
  }, [draft]);

  const displayError = saveError ?? loadError;

  if (loading && draft.length === 0 && !displayError) {
    return <div className="p-6 text-sm text-muted-foreground">Loading quick actions…</div>;
  }

  return (
    <div className="space-y-6 p-6">
      <div>
        <h2 className="text-sm font-semibold">Quick actions</h2>
        <p className="mt-1 text-xs text-muted-foreground">
          The pills below the chat composer. Drag a row to reorder — this order is the order the
          pills appear in. &quot;Fill the composer&quot; puts the message in the input without
          sending it; &quot;Send immediately&quot; dispatches it like you typed it.
        </p>
      </div>

      {displayError && (
        <div
          role="alert"
          className="rounded border border-red-800/60 bg-red-950/30 px-3 py-2 text-xs text-red-300"
        >
          {displayError}
        </div>
      )}

      <p className="text-xs text-muted-foreground">
        {draft.length} of {QUICK_ACTIONS_MAX} chips.
      </p>

      <DndContext sensors={dndSensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
        <SortableContext items={draft.map((c) => c.id)} strategy={rectSortingStrategy}>
          <ul className="space-y-2">
            {draft.map((chip, index) => (
              <ChipRow
                key={chip.id}
                chip={chip}
                index={index}
                onChange={updateChip}
                onDelete={deleteChip}
              />
            ))}
          </ul>
        </SortableContext>
      </DndContext>

      {draft.length === 0 && (
        <p className="text-xs text-muted-foreground">No chips — the composer shows no strip.</p>
      )}

      <div className="flex items-center justify-between gap-3 border-t border-border pt-4">
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={addChip}
          disabled={draft.length >= QUICK_ACTIONS_MAX}
        >
          Add chip
        </Button>
        <Button type="button" size="sm" onClick={() => void onSave()} disabled={saving}>
          {saving ? "Saving…" : "Save changes"}
        </Button>
      </div>
    </div>
  );
}
