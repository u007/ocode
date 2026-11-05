Proposal: Recursive learning for ocode (TUI + web/desktop)
Level: design proposal (decisions and contracts, no implementation). Status: NOT STARTED, nothing here is built.
Date: 2026-11-05. Risk: medium (new persistent store, new prompt tail, a sub-agent that writes guidance the
model later follows; prompt-injection and cache-stability surface).

## The one-paragraph version

ocode already has every raw ingredient of a learning loop (layered memory, a memory-maintenance planner,
compaction summaries that extract user corrections, the Kaizen offline eval → derived-skill pipeline, an OKF
bundle with a sole-writer sub-agent, a scheduler that runs full agents, Jev decision slots), but nothing closes
the loop: no signals are captured, corrections die with the session, the only automatic writer runs in the
TUI only, and nothing ever checks whether a stored "lesson" helped. The proposal adds one new concept, the
**lesson** (a conditional, evidence-backed guidance record with a lifecycle `candidate → trial → active →
archived`), and four stages around it: **capture** signals on every host, **reflect** them into candidate
lessons with a sole-writer sub-agent, **gate** candidates with a Jev slot plus user approval, and **inject**
active lessons as a bounded user-role tail. The recursion is the gate: a lesson stays active only while the
signals it was meant to fix keep improving in the sessions it was injected into. Harness²'s expensive
Propose-Probe-Compose is reserved for scheduled jobs, the one place ocode re-runs the same task.

## Parts

| part | what it answers |
|---|---|
| `01-inventory.md` | What ocode has today, with the gaps and the three bugs a loop would inherit |
| `02-external-analysis.md` | What Harness² (google-research), Recuris, RHI and DeepSeek Harness actually do, and what transfers |
| `03-design.md` | The lesson model, the four stages, invariants, cache and safety rules |
| `04-surfaces.md` | TUI, web/desktop and API surfaces, host parity |
| `05-phases.md` | Phased delivery, prerequisites, cost, risks, open decisions |

Each part stands alone; none refers to another.
