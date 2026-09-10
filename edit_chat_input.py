with open('web/src/components/Chat/ChatInput.tsx', 'r') as f:
    content = f.read()
content = content.replace('from "react";\n// test', 'from "react";')
old_import = 'import { useState, type KeyboardEvent, useRef, useEffect, useCallback } from "react";'
new_import = 'import { useState, type KeyboardEvent, useRef, useEffect, useCallback, forwardRef, useImperativeHandle } from "react";'
content = content.replace(old_import, new_import)
with open('web/src/components/Chat/ChatInput.tsx', 'w') as f:
    f.write(content)
