with open('web/src/components/Chat/ChatInput.tsx', 'r') as f:
    content = f.read()
old_def = 'export default forwardRef<ChatInputHandle, ChatInputProps>(function ChatInput({\n  onSlashCommand,\n  activeEditorContext,\n  contextFilePaths,\n  sessionTabId,\n  onSessionCreated,\n  previewContext,\n  onClearPreviewContext,\n  isActive,\n}: ChatInputProps) {'
new_def = 'export default forwardRef<ChatInputHandle, ChatInputProps>(function ChatInput(\n  { onSlashCommand, activeEditorContext, contextFilePaths, sessionTabId, onSessionCreated, previewContext, onClearPreviewContext, isActive }: ChatInputProps,\n  ref: React.ForwardedRef<ChatInputHandle>\n) {'
content = content.replace(old_def, new_def)
with open('web/src/components/Chat/ChatInput.tsx', 'w') as f:
    f.write(content)
