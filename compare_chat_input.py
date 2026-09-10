with open("web/src/components/Chat/ChatInput.tsx", "r") as f:
    new_content = f.read()

import subprocess
original = subprocess.run(["git", "show", "HEAD:web/src/components/Chat/ChatInput.tsx"], capture_output=True, text=True)
original_content = original.stdout

new_lines = new_content.splitlines()
orig_lines = original_content.splitlines()

print(f"New: {len(new_lines)}, Original: {len(orig_lines)}")

for i in range(min(len(new_lines), len(orig_lines))):
    if new_lines[i] != orig_lines[i]:
        print(f"First difference at line {i+1}:")
        print(f"  NEW: {new_lines[i]}")
        print(f"  ORIG: {orig_lines[i]}")
        break
else:
    print("Files have same content up to shorter length")
    # Print last 20 lines of new
    print("Last 20 new lines:")
    for line in new_lines[-20:]:
        print(f"  {line}")
