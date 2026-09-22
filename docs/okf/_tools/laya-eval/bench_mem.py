import os, sys, time, json, resource, gc
os.environ.setdefault("HF_HUB_OFFLINE","1"); os.environ.setdefault("USE_TF","0")
import torch, laya
sub, device, dt = sys.argv[1], sys.argv[2], sys.argv[3]
sub = None if sub=="english" else sub
def rss(): return resource.getrusage(resource.RUSAGE_SELF).ru_maxrss/1e9
t0=time.time()
a = laya.load("convaiinnovations/laya", subfolder=sub, device=device)
if dt!="fp32":
    a.model.to({"fp16":torch.float16,"bf16":torch.bfloat16}[dt]); a.dtype={"fp16":torch.float16,"bf16":torch.bfloat16}[dt]
load_s=time.time()-t0
Q={"risk":{"type":"choice","instructions":"How risky is this shell command?","criteria":{"safe":"read-only listing or inspection","modifies":"writes files inside the project","dangerous":"deletes data, force-pushes, or touches system"}},
   "done":{"type":"noul","instructions":"Does the assistant say the task is finished?"}}
S="Command: rm -rf node_modules && npm install. Assistant said: I will now reinstall dependencies."
a.predict(S,Q)  # warmup
ts=[]
for _ in range(10):
    t=time.time(); r=a.predict(S,Q); ts.append(time.time()-t)
mps = torch.mps.current_allocated_memory()/1e9 if device=="mps" else 0
print(json.dumps({"ckpt":sub or "english","device":device,"dtype":dt,"load_s":round(load_s,1),"peak_rss_gb":round(rss(),2),"mps_alloc_gb":round(mps,2),"ms_median":round(sorted(ts)[5]*1000),"params_M":round(sum(p.numel() for p in a.model.parameters())/1e6),"max_len":a.cfg.get("max_len"),"head_max_len":a.cfg.get("head_max_len"),"risk":r["answers"]["risk"]["choice"],"done":round(r["answers"]["done"]["noul"],3)}))
