with open("web/src/components/Chat/ChatInput.tsx", "r") as f:
    content = f.read()
old_text = "  /** Called when the user X's the preview chip off this message. */\n  onClearPreviewContext?: () => void;\n}"
new_text = "  /** Called when the user X's the preview chip off this message. */\n  onClearPreviewContext?: () => void;\n  /** Whether this chat input belongs to the currently active session tab. */\n  isActive?: boolean;\n}"
content = content.replace(old_text, new_text)
with open("web/src/components/Chat/ChatInput.tsx", "w") as f:
    f.write(content)
print("Added isActive prop")
