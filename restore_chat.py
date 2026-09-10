import subprocess

# Restore original file from git
result = subprocess.run(
    ["git", "show", "HEAD:web/src/components/Chat/ChatInput.tsx"],
    capture_output=True, text=True, cwd="/Users/james/www/ocode"
)
original_content = result.stdout

with open("/Users/james/www/ocode/web/src/components/Chat/ChatInput.tsx", "w") as f:
    f.write(original_content)

print("Restored original ChatInput.tsx")
