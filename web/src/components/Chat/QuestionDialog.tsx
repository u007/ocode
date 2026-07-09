import { useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import {
  HelpCircle,
  Circle,
  CircleDot,
  Square,
  CheckSquare,
  Send,
} from "lucide-react";
import type {
  QuestionPrompt,
  QuestionAnswerPayload,
  QuestionAnswerValue,
} from "@/api/types";

interface Props {
  open: boolean;
  requestId: string;
  questions: QuestionPrompt[];
  onSubmit: (
    requestId: string,
    answers: QuestionAnswerPayload[],
  ) => Promise<boolean>;
}

const OTHER_LABEL = "Something else";

// Mirrors the TUI's isQuestionOtherLabel so a question that already carries an
// "other/custom" option reuses it as the free-text row rather than getting a
// duplicate appended one.
function isOtherLabel(label: string): boolean {
  const l = label.trim().toLowerCase();
  return (
    l.includes("something else") ||
    l.includes("other") ||
    l.includes("own answer") ||
    l.includes("custom")
  );
}

// Per-question option list with the free-text "Something else" row guaranteed
// present. `custom` marks the free-text row.
interface DisplayOption {
  label: string;
  description?: string;
  custom: boolean;
}

function displayOptions(q: QuestionPrompt): DisplayOption[] {
  const opts: DisplayOption[] = q.options.map((o) => ({
    label: o.label,
    description: o.description,
    custom: isOtherLabel(o.label),
  }));
  if (!opts.some((o) => o.custom)) {
    opts.push({
      label: OTHER_LABEL,
      description: "Type your own answer",
      custom: true,
    });
  }
  return opts;
}

// Per-question selection: a set of chosen option labels plus the custom text.
interface QState {
  selected: Set<string>;
  customText: string;
}

export default function QuestionDialog({
  open,
  requestId,
  questions,
  onSubmit,
}: Props) {
  const optionsPerQuestion = useMemo(
    () => questions.map(displayOptions),
    [questions],
  );

  const [state, setState] = useState<QState[]>(() =>
    questions.map(() => ({ selected: new Set<string>(), customText: "" })),
  );
  const [loading, setLoading] = useState(false);

  const toggle = (qi: number, opt: DisplayOption) => {
    setState((prev) => {
      const next = prev.map((s) => ({
        selected: new Set(s.selected),
        customText: s.customText,
      }));
      const s = next[qi];
      const multiple = !!questions[qi].multiple;
      if (multiple) {
        if (s.selected.has(opt.label)) s.selected.delete(opt.label);
        else s.selected.add(opt.label);
      } else {
        // Radio: exactly one selection.
        s.selected = new Set([opt.label]);
      }
      return next;
    });
  };

  const setCustomText = (qi: number, text: string) => {
    setState((prev) => {
      const next = prev.map((s) => ({
        selected: new Set(s.selected),
        customText: s.customText,
      }));
      next[qi].customText = text;
      return next;
    });
  };

  // A question is answered once it has a selection whose custom row (if chosen)
  // carries non-empty text.
  const isAnswered = (qi: number): boolean => {
    const s = state[qi];
    if (s.selected.size === 0) return false;
    for (const label of s.selected) {
      const opt = optionsPerQuestion[qi].find((o) => o.label === label);
      if (opt?.custom) {
        if (s.customText.trim() === "") return false;
        continue;
      }
      return true;
    }
    // Only the custom row is selected — answered iff its text is filled.
    return s.customText.trim() !== "";
  };

  const allAnswered = questions.every((_, qi) => isAnswered(qi));

  const buildPayload = (): QuestionAnswerPayload[] =>
    questions.map((q, qi) => {
      const s = state[qi];
      const answers: QuestionAnswerValue[] = [];
      for (const opt of optionsPerQuestion[qi]) {
        if (!s.selected.has(opt.label)) continue;
        if (opt.custom) {
          answers.push({
            label: opt.label,
            text: s.customText.trim(),
            custom: true,
          });
        } else {
          answers.push({ label: opt.label });
        }
      }
      return { header: q.header, question: q.question, answers };
    });

  const handleSubmit = async () => {
    if (!allAnswered) return;
    setLoading(true);
    try {
      const ok = await onSubmit(requestId, buildPayload());
      if (!ok) {
        setLoading(false);
      }
    } catch {
      setLoading(false);
    }
  };

  const renderQuestion = (q: QuestionPrompt, qi: number) => {
    const s = state[qi];
    const multiple = !!q.multiple;
    return (
      <div className="space-y-3">
        <p className="text-sm text-zinc-200">{q.question}</p>
        <div className="space-y-1">
          {optionsPerQuestion[qi].map((opt) => {
            const checked = s.selected.has(opt.label);
            const Icon = multiple
              ? checked
                ? CheckSquare
                : Square
              : checked
                ? CircleDot
                : Circle;
            return (
              <div key={opt.label} className="space-y-1">
                <button
                  type="button"
                  onClick={() => toggle(qi, opt)}
                  disabled={loading}
                  className={`flex w-full items-start gap-2 rounded-md border p-2 text-left transition-colors ${
                    checked
                      ? "border-blue-500 bg-blue-500/10"
                      : "border-zinc-700 bg-zinc-800 hover:bg-zinc-750"
                  }`}
                >
                  <Icon
                    className={`mt-0.5 h-4 w-4 flex-shrink-0 ${checked ? "text-blue-400" : "text-zinc-500"}`}
                  />
                  <span className="min-w-0">
                    <span className="block text-sm text-zinc-100">
                      {opt.label}
                    </span>
                    {opt.description && (
                      <span className="block text-xs text-zinc-400">
                        {opt.description}
                      </span>
                    )}
                  </span>
                </button>
                {opt.custom && checked && (
                  <Input
                    autoFocus
                    value={s.customText}
                    onChange={(e) => setCustomText(qi, e.target.value)}
                    placeholder="Type your answer…"
                    disabled={loading}
                    className="ml-6 bg-zinc-900 text-zinc-100"
                  />
                )}
              </div>
            );
          })}
        </div>
      </div>
    );
  };

  return (
    // Non-dismissible: closing without an answer would leave the agent paused,
    // so the dialog stays until Submit resolves it server-side.
    <Dialog open={open}>
      <DialogContent
        className="sm:max-w-lg bg-zinc-900 border-zinc-700"
        onInteractOutside={(e) => e.preventDefault()}
      >
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-zinc-100">
            <HelpCircle className="w-5 h-5 text-blue-400" />
            {questions.length === 1
              ? questions[0].header || "Question"
              : "Questions"}
          </DialogTitle>
        </DialogHeader>

        {questions.length === 1 ? (
          renderQuestion(questions[0], 0)
        ) : (
          <Tabs defaultValue="0" className="w-full">
            <TabsList className="flex-wrap">
              {questions.map((q, qi) => (
                <TabsTrigger key={qi} value={String(qi)}>
                  {(q.header || `Question ${qi + 1}`) +
                    (isAnswered(qi) ? " ✓" : "")}
                </TabsTrigger>
              ))}
            </TabsList>
            {questions.map((q, qi) => (
              <TabsContent key={qi} value={String(qi)} className="mt-3">
                {renderQuestion(q, qi)}
              </TabsContent>
            ))}
          </Tabs>
        )}

        <div className="flex justify-end pt-2">
          <Button
            type="button"
            onClick={() => void handleSubmit()}
            disabled={loading || !allAnswered}
          >
            <Send className="w-4 h-4 mr-2" />
            Submit
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
