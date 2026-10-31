import type { Step } from "../tools/journal";

/**
 * A step belongs to the current turn when it was written at or after the moment the user last pressed Send.
 * `turnStartMs` is null before any Send, so no step is this turn yet.
 */
export function isThisTurn(step: Step, turnStartMs: number | null): boolean {
  return turnStartMs !== null && Date.parse(step.at) >= turnStartMs;
}

/** Every journal step, newest first, with Undo and Show. Steps from the current turn are highlighted. */
export function ChangesView({ steps, turnStart, onUndo, onShow }: {
  steps: Step[];
  turnStart: number | null;
  onUndo: (id: string) => void;
  onShow: (step: Step) => void;
}) {
  const turnCount = steps.filter((s) => isThisTurn(s, turnStart)).length;
  return (
    <div className="changes-tab">
      <div className="logbar">
        <span className="muted">{turnCount} {turnCount === 1 ? "change" : "changes"} this turn</span>
      </div>
      {steps.length === 0 && <div className="msg tool">No changes yet. Every write is checkpointed here and can be undone.</div>}
      <ul className="change-list">
        {steps.map((s) => {
          const thisTurn = isThisTurn(s, turnStart);
          return (
            <li key={s.id} className={`${thisTurn ? "this-turn" : ""} ${s.undone ? "undone" : ""}`}>
              <span className="time">{new Date(s.at).toLocaleTimeString([], { hourCycle: "h23" })}</span>
              <span className="what">
                <b>{s.tool}</b> {s.sheet}!{s.address}{thisTurn && <span className="badge">this turn</span>}
              </span>
              <button onClick={() => onShow(s)}>Show</button>
              <button disabled={s.undone} onClick={() => onUndo(s.id)}>Undo</button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
