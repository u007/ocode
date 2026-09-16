#!/usr/bin/env python3
# Add projectHost to EditorTab and thread it through open/save/reload paths.
path = "web/src/hooks/useEditorTabs.ts"
src = open(path).read()

# 1. EditorTab interface: add projectHost after projectRoot
old = """export interface EditorTab {
  id: string;
  path: string;
  projectRoot?: string;
  content: string;"""
new = """export interface EditorTab {
  id: string;
  path: string;
  projectRoot?: string;
  /** Registered remote target (SSH/WSL) for this tab's project; routes the
   *  content fetch/save and git decorations to the remote host. Empty for
   *  local projects. */
  projectHost?: string;
  content: string;"""
assert src.count(old) == 1
src = src.replace(old, new)

# 2. handleOpenFile signature: accept host, store on tab, pass to fetch
old = """  const handleOpenFile = useCallback(async (path: string, projectRoot?: string) => {"""
new = """  const handleOpenFile = useCallback(async (path: string, projectRoot?: string, host?: string) => {"""
assert src.count(old) == 1
src = src.replace(old, new)

# 3. fetch call inside handleOpenFile
old = """      const disk = await fetchFileContent(path, projectRoot);"""
new = """      const disk = await fetchFileContent(path, projectRoot, host);"""
assert src.count(old) == 1
src = src.replace(old, new)

# 4. Tab constructions inside handleOpenFile: add projectHost after projectRoot
count = src.count("""          id,
          path,
          projectRoot,
          content:""")
assert count == 1, count
src = src.replace("""          id,
          path,
          projectRoot,
          content:""", """          id,
          path,
          projectRoot,
          projectHost: host,
          content:""")
count = src.count("""        tab = {
          id,
          path,
          projectRoot,
          content: disk.content,""")
assert count == 1, count
src = src.replace("""        tab = {
          id,
          path,
          projectRoot,
          content: disk.content,""", """        tab = {
          id,
          path,
          projectRoot,
          projectHost: host,
          content: disk.content,""")

# 5. saveEditorTab: pass host
old = """        await api.saveFileContent(tab.path, tab.content, tab.projectRoot, expectedHash, opts?.force);"""
new = """        await api.saveFileContent(tab.path, tab.content, tab.projectRoot, expectedHash, opts?.force, tab.projectHost);"""
assert src.count(old) == 1
src = src.replace(old, new)

open(path, "w").write(src)
print("editor tabs patched")