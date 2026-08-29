import { useEffect, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Progress } from "@/components/ui/progress";
import { Loader2 } from "lucide-react";
import { api } from "@/api/client";
import { eventBus } from "@/lib/eventBus";

type Mode = "encrypt" | "decrypt";
type Step = "loading" | "confirm" | "passphrase" | "progress" | "done" | "error";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  path: string;
  name: string;
  isDir: boolean;
  mode: Mode;
}

// SecretActionDialog drives one encrypt/decrypt operation on a file or
// directory: a directory first shows an exact file-count confirmation (from
// GET /api/secret/scan), then a passphrase step (encrypt also requires
// retyping it), then either an immediate result (single file) or a
// cancellable progress view driven by secret_progress/secret_done/
// secret_error/secret_cancelled events on the shared event bus.
export default function SecretActionDialog({ open, onOpenChange, path, name, isDir, mode }: Props) {
  const [step, setStep] = useState<Step>("loading");
  const [fileCount, setFileCount] = useState<number | null>(null);
  const [passphrase, setPassphrase] = useState("");
  const [confirmPassphrase, setConfirmPassphrase] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [jobId, setJobId] = useState<string | null>(null);
  const [progress, setProgress] = useState({ done: 0, total: 0, current: "" });

  useEffect(() => {
    if (!open) return;
    setPassphrase("");
    setConfirmPassphrase("");
    setError("");
    setJobId(null);
    setProgress({ done: 0, total: 0, current: "" });

    if (!isDir) {
      setStep("passphrase");
      return;
    }
    setStep("loading");
    api
      .secretScan(path, mode)
      .then((res) => {
        setFileCount(res.file_count ?? 0);
        setStep("confirm");
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Failed to scan directory");
        setStep("error");
      });
  }, [open, path, isDir, mode]);

  useEffect(() => {
    if (step !== "progress" || !jobId) return;
    const offProgress = eventBus.on("secret_progress", (env) => {
      const data = env.data as { job_id: string; done: number; total: number; current?: string };
      if (data.job_id !== jobId) return;
      setProgress({ done: data.done, total: data.total, current: data.current ?? "" });
    });
    const offDone = eventBus.on("secret_done", (env) => {
      const data = env.data as { job_id: string };
      if (data.job_id !== jobId) return;
      setStep("done");
    });
    const offError = eventBus.on("secret_error", (env) => {
      const data = env.data as { job_id: string; error: string };
      if (data.job_id !== jobId) return;
      setError(data.error);
      setStep("error");
    });
    const offCancelled = eventBus.on("secret_cancelled", (env) => {
      const data = env.data as { job_id: string };
      if (data.job_id !== jobId) return;
      setStep("done");
    });
    return () => {
      offProgress();
      offDone();
      offError();
      offCancelled();
    };
  }, [step, jobId]);

  const verb = mode === "encrypt" ? "Encrypt" : "Decrypt";

  // The directory whose .ocode/secret.key.age owns path — same rule the CLI
  // uses (a file's containing dir, or the dir itself).
  const projectAnchor = isDir ? path : path.replace(/[\\/][^\\/]*$/, "") || path;

  const runEncrypt = () => api.secretEncrypt(path, passphrase, confirmPassphrase);

  const submit = async () => {
    if (!passphrase) return;
    if (mode === "encrypt" && passphrase !== confirmPassphrase) {
      setError("Passphrases did not match");
      return;
    }
    setSubmitting(true);
    setError("");
    try {
      const res = mode === "encrypt" ? await runEncrypt() : await api.secretDecrypt(path, passphrase);
      if (res.status === "started" && res.job_id) {
        setJobId(res.job_id);
        setProgress({ done: 0, total: res.total ?? 0, current: "" });
        setStep("progress");
      } else {
        setStep("done");
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : `Failed to ${mode}`;
      // First encrypt in a project with no key yet: set one up using the
      // passphrase already entered, then retry once, instead of dead-ending
      // on an error only the CLI/TUI could resolve.
      if (mode === "encrypt" && message.includes("run secret init first")) {
        try {
          await api.secretInit(projectAnchor, passphrase, confirmPassphrase);
          const res = await runEncrypt();
          if (res.status === "started" && res.job_id) {
            setJobId(res.job_id);
            setProgress({ done: 0, total: res.total ?? 0, current: "" });
            setStep("progress");
          } else {
            setStep("done");
          }
          return;
        } catch (initErr) {
          setError(initErr instanceof Error ? initErr.message : "Failed to set up encryption");
          setSubmitting(false);
          return;
        }
      }
      setError(message);
    } finally {
      setSubmitting(false);
    }
  };

  const cancelJob = async () => {
    if (!jobId) return;
    try {
      await api.secretCancel(jobId);
    } catch {
      // The job may have already finished; the secret_done/secret_error
      // event that races this is still the source of truth for the step.
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !submitting && step !== "progress" && onOpenChange(next)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {verb} {isDir ? "directory" : "file"}
          </DialogTitle>
          <DialogDescription className="truncate">{name}</DialogDescription>
        </DialogHeader>

        {step === "loading" && (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="w-5 h-5 text-muted-foreground animate-spin" />
          </div>
        )}

        {step === "confirm" && (
          <>
            <p className="text-sm text-muted-foreground">
              {fileCount === 0
                ? `No files to ${mode} under this directory.`
                : `${verb} ${fileCount} file${fileCount === 1 ? "" : "s"} under this directory? This cannot be undone from here.`}
            </p>
            <DialogFooter>
              <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              {fileCount !== 0 && (
                <Button size="sm" onClick={() => setStep("passphrase")}>
                  Continue
                </Button>
              )}
            </DialogFooter>
          </>
        )}

        {step === "passphrase" && (
          <>
            <div className="space-y-2">
              <Input
                type="password"
                placeholder="Passphrase"
                value={passphrase}
                onChange={(e) => setPassphrase(e.target.value)}
                autoFocus
              />
              {mode === "encrypt" && (
                <Input
                  type="password"
                  placeholder="Confirm passphrase"
                  value={confirmPassphrase}
                  onChange={(e) => setConfirmPassphrase(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && submit()}
                />
              )}
              {error && <p className="text-xs text-destructive">{error}</p>}
            </div>
            <DialogFooter>
              <Button variant="outline" size="sm" onClick={() => onOpenChange(false)} disabled={submitting}>
                Cancel
              </Button>
              <Button
                size="sm"
                onClick={submit}
                disabled={
                  submitting ||
                  !passphrase ||
                  (mode === "encrypt" && !confirmPassphrase)
                }
              >
                {submitting && <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" />}
                {verb}
              </Button>
            </DialogFooter>
          </>
        )}

        {step === "progress" && (
          <>
            <div className="space-y-2">
              <Progress value={progress.total ? (progress.done / progress.total) * 100 : 0} />
              <p className="text-xs text-muted-foreground truncate">
                {progress.done}/{progress.total} {progress.current && `— ${progress.current}`}
              </p>
            </div>
            <DialogFooter>
              <Button variant="outline" size="sm" onClick={cancelJob}>
                Cancel
              </Button>
            </DialogFooter>
          </>
        )}

        {step === "done" && (
          <>
            <p className="text-sm text-muted-foreground">
              {verb === "Encrypt" ? "Encrypted" : "Decrypted"} successfully.
            </p>
            <DialogFooter>
              <Button size="sm" onClick={() => onOpenChange(false)}>
                Close
              </Button>
            </DialogFooter>
          </>
        )}

        {step === "error" && (
          <>
            <p className="text-sm text-destructive">{error}</p>
            <DialogFooter>
              <Button size="sm" onClick={() => onOpenChange(false)}>
                Close
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
