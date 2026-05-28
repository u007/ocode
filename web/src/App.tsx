import { useState } from "react";
import { ChatProvider, useChatDispatch } from "./stores/chatStore";
import ErrorBoundary from "./components/common/ErrorBoundary";
import ChatPanel from "./components/Chat/ChatPanel";
import ChatInput from "./components/Chat/ChatInput";
import SessionList from "./components/Sidebar/SessionList";
import ModelSelector from "./components/Sidebar/ModelSelector";
import AgentTabs from "./components/Sidebar/AgentTabs";
import StatusBar from "./components/common/StatusBar";
import CommandPalette from "./components/common/CommandPalette";
import GitPanel from "./components/Git/GitPanel";
import FileTree from "./components/Files/FileTree";
import { useKeyboard } from "./hooks/useKeyboard";

function AppInner() {
  const [activeAgent, setActiveAgent] = useState("coder");
  const [cmdOpen, setCmdOpen] = useState(false);
  const dispatch = useChatDispatch();

  useKeyboard({
    onNewSession: () => dispatch({ type: "RESET" }),
    onCommandPalette: () => setCmdOpen(true),
    onEscape: () => setCmdOpen(false),
  });

  const handleCommand = (cmd: string) => {
    if (cmd === "/clear") dispatch({ type: "RESET" });
  };

  return (
    <div className="flex h-screen">
      <aside className="w-64 flex-shrink-0 border-r border-zinc-700 bg-zinc-900 flex flex-col">
        <SessionList />
        <ModelSelector />
        <AgentTabs activeAgent={activeAgent} onSelect={setActiveAgent} />
        <GitPanel />
        <FileTree />
      </aside>
      <main className="flex flex-1 flex-col">
        <ChatPanel />
        <ChatInput />
        <StatusBar />
      </main>
      <CommandPalette
        open={cmdOpen}
        onClose={() => setCmdOpen(false)}
        onExecute={handleCommand}
      />
    </div>
  );
}

export default function App() {
  return (
    <ErrorBoundary>
      <ChatProvider>
        <AppInner />
      </ChatProvider>
    </ErrorBoundary>
  );
}
