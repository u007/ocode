import json, sqlite3, glob, os, sys, random, re
base=os.path.expanduser("~/.local/share/opencode/project")
slug2path={"dcd5a911f8bd":"/Users/james/www/ocode","0e76e28517a8":"/Users/james/www/aimsai2","a82db8cf8f04":"/Users/james/www/aimsai","17dadbde673f":"/Users/james/www/ecomm"}
def read_session(f):
    msgs=[]
    if f.endswith(".sqlite"):
        try:
            for (d,) in sqlite3.connect(f).execute("select data from messages order by seq"): msgs.append(json.loads(d))
        except Exception as e: return []
    else:
        for i,line in enumerate(open(f,errors="replace")):
            if i==0: continue
            try: r=json.loads(line)
            except: continue
            if r.get("type")=="msg": msgs.append(r)
    return msgs
files=[]
for slug in slug2path:
    files+=sorted(glob.glob(f"{base}/{slug}/sessions/ses_*.sqlite"),key=os.path.getmtime)[-150:]
    files+=sorted(glob.glob(f"{base}/{slug}/sessions/*.ojsonl"),key=os.path.getmtime)[-150:]
bash=[]; tails=[]
for f in files:
    slug=f.split("/")[-3]; msgs=read_session(f)
    for i,m in enumerate(msgs):
        for tc in (m.get("tool_calls") or []):
            fn=tc.get("function",{})
            if fn.get("name")=="bash":
                try: cmd=json.loads(fn.get("arguments","{}")).get("command","")
                except: continue
                if cmd: bash.append({"session":os.path.basename(f),"slug":slug,"workdir":slug2path[slug],"command":cmd})
        if m.get("notice") and "auto-continue" in m["notice"]:
            # tail = 6 msgs before notice
            prev=[x for x in msgs[max(0,i-6):i] if (x.get("content") or "").strip()]
            tails.append({"session":os.path.basename(f),"label":"continue","notice":m["notice"],"tail":[{"role":x["role"],"content":x["content"][:4000]} for x in prev]})
    # natural endings: last assistant text message before a user message (not tool)
    for i in range(1,len(msgs)):
        if msgs[i].get("role")=="user" and not msgs[i].get("tool_call_id") and (msgs[i].get("content") or "").strip() and not msgs[i]["content"].startswith("Continue the task"):
            j=i-1
            while j>=0 and msgs[j].get("role")!="assistant": j-=1
            if j>=0 and (msgs[j].get("content") or "").strip() and not msgs[j].get("tool_calls"):
                prev=[x for x in msgs[max(0,j-5):j+1] if (x.get("content") or "").strip()]
                tails.append({"session":os.path.basename(f),"label":"end?","tail":[{"role":x["role"],"content":x["content"][:4000]} for x in prev]})
json.dump(bash,open("bash_all.json","w"),indent=1); json.dump(tails,open("tails_all.json","w"),indent=1)
print(len(files),"files",len(bash),"bash calls",len(tails),"tails", sum(1 for t in tails if t["label"]=="continue"),"continue-labelled")
