import { ChatProvider } from "./stores/chatStore";
import ChatPanel from "./components/Chat/ChatPanel";
import ChatInput from "./components/Chat/ChatInput";

export default function App() {
  return (
    <ChatProvider>
      <div className="flex h-screen flex-col">
        <ChatPanel />
        <ChatInput />
      </div>
    </ChatProvider>
  );
}
