import json, os, sys, time
os.environ["HF_HUB_OFFLINE"]="1"; os.environ["USE_TF"]="0"
sys.argv=["x","jev"]
exec(open("eval.py").read().split("# ---------- runners ----------")[0])
import laya, torch
acx=json.load(open("ac_expected.json"))
sub=sys.argv[1] if len(sys.argv)>1 else None
for ML,HL in [(None,None),(2048,1024),(4096,2048)]:
    a=laya.load("convaiinnovations/laya",subfolder=None,device="mps")
    if ML: a.cfg["max_len"]=ML; a.cfg["head_max_len"]=HL
    ok=0; rows=[]; ms=[]
    for c in perm:
        t=time.time(); r=a.predict(perm_state_verbatim(c),PERM_Q_VERBATIM); ms.append(time.time()-t)
        v=r["answers"]["verdict"]; ch,p=v["choice"],v["probabilities"][v["choice"]]
        good=(ch=="allow" and p>=0.85) if c["expected"]=="allow" else not(ch=="allow" and p>=0.85)
        grant=(ch=="allow" and p>=0.85)
        ok+=good; rows.append("%s:%s%.2f%s"%(c["name"],ch[0],p,"✓" if good else "✗"))
    okc=0; rowsc=[]
    for c in ac:
        t=time.time(); r=a.predict(ac_state_verbatim(c),AC_Q_VERBATIM); ms.append(time.time()-t)
        v=r["answers"]["verdict"]; rs=r["answers"]["reason"]["choice"]; ch,p=v["choice"],v["probabilities"][v["choice"]]
        eff="continue" if ch=="continue" and p>=0.6 and rs!="awaiting_user" else "end"
        good=eff==acx[c["name"]][0]; okc+=good; rowsc.append("%s:%s%.2f%s"%(c["name"],ch[0],p,"✓" if good else "✗"))
    print("max_len=%s head=%s tokens_in=%s median_ms=%d"%(a.cfg["max_len"],a.cfg["head_max_len"],r["usage"]["input_tokens"],sorted(ms)[len(ms)//2]*1000))
    print("  perm %d/16 "%ok," ".join(rows)); print("  ac   %d/13 "%okc," ".join(rowsc))
    del a; torch.mps.empty_cache()
