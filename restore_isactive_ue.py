with open("web/src/components/Chat/ChatInput.tsx", "r") as f:
    content = f.read()

old_text = "  useImperativeHandle(ref, () => ({\n    focus: () => {\n      textareaRef.current?.focus();\n    },\n  }));"
new_text = "  useImperativeHandle(ref, () => ({\n    focus: () => {\n      textareaRef.current?.focus();\n    },\n  }));\n\n  // Auto-focus the chat input when this session tab becomes active.\n  useEffect(() => {\n    if (isActive && textareaRef.current) {\n      textareaRef.current.focus();\n    }\n  }, [isActive]);"
content = content.replace(old_text, new_text)
with open("web/src/components/Chat/ChatInput.tsx", "w") as f:
    f.write(content)
print("Restored isActive useEffect")
