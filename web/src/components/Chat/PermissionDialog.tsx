import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { AlertTriangle, Terminal, FileCode, Check, X } from "lucide-react";

interface Props {
  open: boolean;
  tool: string;
  command?: string;
  requestId: string;
  onApprove: (requestId: string, approved: boolean) => Promise<boolean>;
}

export default function PermissionDialog({
  open,
  tool,
  command,
  requestId,
  onApprove,
}: Props) {
  const [loading, setLoading] = useState(false);

  const handleResponse = async (approved: boolean) => {
    setLoading(true);
    try {
      const ok = await onApprove(requestId, approved);
      if (!ok) {
        setLoading(false);
      }
    } catch {
      setLoading(false);
    }
  };

  const toolIcon =
    tool === "bash" || tool === "bash_output" ? Terminal : FileCode;

  return (
    <Dialog
      open={open}
      onOpenChange={(isOpen) => {
        if (!isOpen && !loading) {
          void handleResponse(false);
        }
      }}
    >
      <DialogContent className="sm:max-w-md bg-zinc-900 border-zinc-700">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-zinc-100">
            <AlertTriangle className="w-5 h-5 text-yellow-400" />
            Permission Required
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          <div className="flex items-center gap-3 p-3 rounded-lg bg-zinc-800 border border-zinc-700">
            {(() => {
              const Icon = toolIcon;
              return <Icon className="w-5 h-5 text-blue-400 flex-shrink-0" />;
            })()}
            <div>
              <div className="text-sm font-medium text-zinc-200">
                The agent wants to use{" "}
                <span className="font-mono text-blue-400">{tool}</span>
              </div>
              {command && (
                <div className="mt-2 text-xs text-zinc-400 font-mono bg-zinc-900 p-2 rounded overflow-x-auto max-h-32">
                  {command}
                </div>
              )}
            </div>
          </div>

          <div className="flex justify-end gap-3">
            <Button
              type="button"
              variant="destructive"
              onClick={() => void handleResponse(false)}
              disabled={loading}
            >
              <X className="w-4 h-4 mr-2" />
              Deny
            </Button>
            <Button
              type="button"
              onClick={() => void handleResponse(true)}
              disabled={loading}
            >
              <Check className="w-4 h-4 mr-2" />
              Approve
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
