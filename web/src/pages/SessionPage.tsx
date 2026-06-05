import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { api } from "../api/client";
import { useChatDispatch } from "../stores/chatStore";
import ChatPanel from "../components/Chat/ChatPanel";
import AgentPreview from "../components/Chat/AgentPreview";
import ChatInput from "../components/Chat/ChatInput";
import StatusBar from "../components/common/StatusBar";
import CoworkSidebar from "../components/Layout/CoworkSidebar";
import ModelDialog from "../components/Layout/ModelDialog";
import PermissionDialog from "../components/Chat/PermissionDialog";
import { useChat } from "../hooks/useChat";

type ModelDialogTab = "main" | "small" | "advisor";

export default function SessionPage() {
  const { id } = useParams<{ id: string }>();
  const dispatch = useChatDispatch();
  const { resolvePermission, pendingPermission } = useChat();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [coworkOpen, setCoworkOpen] = useState(true);
  const [modelDialogOpen, setModelDialogOpen] = useState(false);
  const [modelDialogTab, setModelDialogTab] = useState<ModelDialogTab>("main");

  useEffect(() => {
    if (!id) return;
    setLoading(true);
    setError(null);
    api
      .getSession(id)
      .then((session) => {
        dispatch({ type: "SET_SESSION", sessionId: id });
        dispatch({ type: "SET_MESSAGES", messages: session.messages || [] });
      })
      .catch((err) => {
        setError(err.message || "Failed to load session");
      })
      .finally(() => setLoading(false));
  }, [id, dispatch]);

  // Seed the advisor on/off state from the server.
  useEffect(() => {
    api
      .getAdvisorEnabled()
      .then((res) =>
        dispatch({ type: "SET_ADVISOR_ENABLED", enabled: res.enabled }),
      )
      .catch(console.error);
  }, [dispatch]);

  const openModelDialog = (tab: ModelDialogTab = "main") => {
    setModelDialogTab(tab);
    setModelDialogOpen(true);
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-screen bg-zinc-950 text-zinc-400">
        Loading session…
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center h-screen bg-zinc-950 text-red-400">
        {error}
      </div>
    );
  }

  return (
    <div className="flex flex-col h-screen bg-zinc-950">
      <div className="flex flex-1 overflow-hidden">
        <main className="flex flex-1 flex-col overflow-hidden">
          <ChatPanel />
          <AgentPreview />
          <ChatInput />
          <StatusBar
            onCoworkToggle={() => setCoworkOpen(!coworkOpen)}
            onModelClick={() => openModelDialog("main")}
          />
        </main>

        <CoworkSidebar
          isOpen={coworkOpen}
          onClose={() => setCoworkOpen(false)}
          activeAgent="build"
          onModelClick={openModelDialog}
        />
      </div>

      <ModelDialog
        open={modelDialogOpen}
        onClose={() => setModelDialogOpen(false)}
        initialTab={modelDialogTab}
      />

      {pendingPermission && (
        <PermissionDialog
          open={true}
          tool={pendingPermission.tool}
          command={pendingPermission.command}
          requestId={pendingPermission.request_id}
          onApprove={resolvePermission}
        />
      )}
    </div>
  );
}
