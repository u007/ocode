import { useState } from "react";
import { ChatProvider } from "./stores/chatStore";
import ChatPanel from "./components/Chat/ChatPanel";
import ChatInput from "./components/Chat/ChatInput";
import SessionList from "./components/Sidebar/SessionList";
import ModelSelector from "./components/Sidebar/ModelSelector";
import AgentTabs from "./components/Sidebar/AgentTabs";

export default function App() {
  const [activeAgent, setActiveAgent] = useState("coder");

  return (
    <ChatProvider>
      <div className="flex h-screen">
        <aside className="w-64 flex-shrink-0 border-r border-zinc-700 bg-zinc-900 flex flex-col">
          <SessionList />
          <ModelSelector />
          <AgentTabs activeAgent={activeAgent} onSelect={setActiveAgent} />
        </aside>
        <main className="flex flex-1 flex-col">
          <ChatPanel />
          <ChatInput />
        </main>
      </div>
    </ChatProvider>
  );
}
