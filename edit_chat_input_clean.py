with open("web/src/components/Chat/ChatInput.tsx", "r") as f:
    content = f.read()

# 1. Update imports
old_import = 'import { useState, type KeyboardEvent, useRef, useEffect, useCallback } from "react";'
new_import = 'import { useState, type KeyboardEvent, useRef, useEffect, useCallback, forwardRef, useImperativeHandle, type ForwardedRef } from "react";'
content = content.replace(old_import, new_import)

# 2. Add ChatInputHandle interface before component
old_interface = 'export interface SlashCommandResult {'
new_interface = 'export interface ChatInputHandle {\n  focus: () => void;\n}\n\nexport interface SlashCommandResult {'
content = content.replace(old_interface, new_interface)

# 3. Change component definition
old_def = 'export default function ChatInput({\n  onSlashCommand,'
new_def = 'export default forwardRef<ChatInputHandle, ChatInputProps>(function ChatInput({\n  onSlashCommand,'
content = content.replace(old_def, new_def)

# 4. Add ref parameter to function
old_sig = ': ChatInputProps) {'
new_sig = ': ChatInputProps,\n  ref: ForwardedRef<ChatInputHandle>\n) {'
content = content.replace(old_sig, new_sig, 1)  # Only replace first occurrence (the component def)

# 5. Add useImperativeHandle after attachRef declaration
old_ref = '  const attachRef = useRef<HTMLInputElement>(null);'
new_ref = '  const attachRef = useRef<HTMLInputElement>(null);\n\n  useImperativeHandle(ref, () => ({\n    focus: () => {\n      textareaRef.current?.focus();\n    },\n  }));'
content = content.replace(old_ref, new_ref)

# 6. Fix closing - the file ends with `}` which needs to become `})`
content = content.rstrip()
if content.endswith("}\n") or content.endswith("}"):
    # Find the last `}` in the file (after JSX closing)
    # The original ends with `}` for the function. Replace last `}` with `})`.
    # But we need to be careful - let's just replace the very last `}` with `})`.
    last_brace_idx = content.rfind('}')
    content = content[:last_brace_idx] + '})' + content[last_brace_idx+1:]

with open("web/src/components/Chat/ChatInput.tsx", "w") as f:
    f.write(content)

print("Applied minimal changes to ChatInput.tsx")
