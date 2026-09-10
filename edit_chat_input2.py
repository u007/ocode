with open('web/src/components/Chat/ChatInput.tsx', 'r') as f:
    content = f.read()

# Change component definition to use forwardRef
old_def = 'export default function ChatInput({\n  onSlashCommand,'
new_def = 'export interface ChatInputHandle {\n  focus: () => void;\n}\n\nexport default forwardRef<ChatInputHandle, ChatInputProps>(function ChatInput({\n  onSlashCommand,'
content = content.replace(old_def, new_def)

# Change closing of component definition from `}` to `})` for forwardRef
# We need to find the right place. Let me use a more targeted approach.

with open('web/src/components/Chat/ChatInput.tsx', 'w') as f:
    f.write(content)
