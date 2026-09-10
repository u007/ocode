with open("web/src/components/Chat/ChatInput.tsx", "r") as f:
    content = f.read()

# 1. Add imports
old_import = 'import { useState, type KeyboardEvent, useRef, useEffect, useCallback } from "react";'
new_import = 'import { useState, type KeyboardEvent, useRef, useEffect, useCallback, forwardRef, useImperativeHandle, type ForwardedRef } from "react";'
content = content.replace(old_import, new_import)

# 2. Add ChatInputHandle export after SlashCommandResult interface
old_result = 'export interface SlashCommandResult {\n  handled: boolean;\n  startedTurn?: boolean;\n  accepted?: boolean;\n}'
new_result = old_result + '\n\nexport interface ChatInputHandle {\n  focus: () => void;\n}'
content = content.replace(old_result, new_result)

# 3. Change component definition to forwardRef
old_def = 'export default function ChatInput({'
new_def = 'export default forwardRef<ChatInputHandle, ChatInputProps>(function ChatInput({'
content = content.replace(old_def, new_def)

# 4. Add ref parameter to function signature
old_sig = '}: ChatInputProps) {'
new_sig = '}: ChatInputProps,\n  ref: ForwardedRef<ChatInputHandle>\n) {'
content = content.replace(old_sig, new_sig)

# 5. Add useImperativeHandle after attachRef line
old_ref_line = '  const attachRef = useRef<HTMLInputElement>(null);'
new_ref_line = old_ref_line + '\n\n  useImperativeHandle(ref, () => ({\n    focus: () => {\n      textareaRef.current?.focus();\n    },\n  }));'
content = content.replace(old_ref_line, new_ref_line)

# 6. Change closing brace to }) for forwardRef
# We need to only replace the very last occurrence of the closing brace pattern
content = content.rstrip()
if content.endswith('}'):
    # Find the last line
    lines = content.split('\n')
    # The last line should be just `}` - replace it with `})`
    last_line_idx = len(lines) - 1
    while last_line_idx >= 0 and lines[last_line_idx].strip() == '':
        last_line_idx -= 1
    if lines[last_line_idx].strip() == '}':
        lines[last_line_idx] = lines[last_line_idx].rstrip() + ')'  # This won't work right
        # Actually just replace the last non-empty line
        lines[last_line_idx] = '})'
    content = '\n'.join(lines)

with open("web/src/components/Chat/ChatInput.tsx", "w") as f:
    f.write(content)
print("Done")
