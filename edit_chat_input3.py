with open('web/src/components/Chat/ChatInput.tsx', 'r') as f:
    content = f.read()

old_block = '  const textareaRef = useRef<HTMLTextAreaElement>(null);\n  const attachRef = useRef<HTMLInputElement>(null);'
new_block = '  const textareaRef = useRef<HTMLTextAreaElement>(null);\n  const attachRef = useRef<HTMLInputElement>(null);\n\n  useImperativeHandle(ref, () => ({\n    focus: () => {\n      textareaRef.current?.focus();\n    },\n  }));'
content = content.replace(old_block, new_block)
with open('web/src/components/Chat/ChatInput.tsx', 'w') as f:
    f.write(content)
