import math, random
G = 9.80665

class Cfg:
    def __init__(s, impact=2.6, ff=0.6, sprint_rms=6.0, sprint_cad=3.0, gyro=3.5, min_fall=0.6, acc_rms=5.0):
        s.impact=impact; s.ff=ff; s.sprint_rms=sprint_rms; s.sprint_cad=sprint_cad
        s.gyro=gyro; s.min_fall=min_fall; s.acc_rms=acc_rms

def norm(v):
    m=math.sqrt(sum(x*x for x in v)) or 1e-9
    return [x/m for x in v]
def angle(a,b):
    d=max(-1,min(1,sum(x*y for x,y in zip(norm(a),norm(b)))))
    return math.degrees(math.acos(d))

def rank(l): return 1 if l==9 else l

class Engine:
    def __init__(s, cfg):
        s.c=cfg; s.buf=[]  # (t, magG, ax,ay,az, gyro)
        s.ff_start=None; s.last_ff_end=None; s.last_ff_dur=0
        s.cand=None; s.fall_cool=-1e9
        s.sm=None; s.last_sm=[None,None]; s.last_t=None
        s.steps=[]; s.peak_ema=0.0; s.last_step=-1e9
        s.dyn_win=[]  # (t,dyn)
        s.gyro_win=[]
        s.next_eval=None
        s.hist=[]  # (t, level)
        s.pend=None; s.cand_level=None; s.cand_n=0; s.activity=0
        s.sprint_cool=-1e9; s.struggle_cool=-1e9; s.struggle_hits=0; s.struggle_emit=-1e9
        s.sprint_start=None; s.sprint_ok=False
        s.fall_emit_t=None; s.last_moving=-1e9; s.inactive_done=True
        s.jerk=[]; s.sm_win=[]
    # levels: 0 still 1 walk 2 run 3 sprint 9 unknown
    def process(s, t, a, g):
        ev=[]
        mag=math.sqrt(sum(x*x for x in a)); magG=mag/G
        gy=math.sqrt(sum(x*x for x in g))
        s.buf.append((t,magG,a,gy)); 
        while s.buf and t-s.buf[0][0]>7000: s.buf.pop(0)
        ev+=s._fall(t,magG,gy)
        ev+=s._gait(t,mag-G,gy,magG)
        if abs(magG-1)>0.25: s.last_moving=t
        ev+=s._inactive(t)
        return ev
    def _fall(s,t,magG,gy):
        c=s.c; out=[]
        if s.cand is None:
            if magG<c.ff:
                if s.ff_start is None: s.ff_start=t
            elif s.ff_start is not None:
                d=t-s.ff_start; s.ff_start=None
                if d>=60: s.last_ff_end=t; s.last_ff_dur=d
            if t-s.fall_cool>0 and magG>=c.impact-0.0:
                has_ff = s.last_ff_end is not None and t-s.last_ff_end<=700
                gpk=max([b[3] for b in s.buf if t-b[0]<=500] or [0])
                if has_ff or (magG>=c.impact+0.8 and gpk>=2.0):
                    pre=[b[2] for b in s.buf if t-2500<=b[0]<=t-700 and 0.85<b[1]<1.15]
                    pre_g = [sum(x[i] for x in pre)/len(pre) for i in range(3)] if len(pre)>=10 else None
                    s.cand=dict(t0=t,peak=magG,ff=has_ff,pre=pre_g)
        else:
            cd=s.cand
            if t-cd['t0']<=200: cd['peak']=max(cd['peak'],magG)
            if t-cd['t0']>=3000:
                w=[b for b in s.buf if cd['t0']+600<=b[0]<=cd['t0']+3000]
                vals=[b[1] for b in w]; n=len(vals)
                mean=sum(vals)/n; std=math.sqrt(sum((v-mean)**2 for v in vals)/n)
                still = std<=0.10 and abs(mean-1)<=0.15
                late=[b[2] for b in s.buf if cd['t0']+1500<=b[0]<=cd['t0']+3000]
                post=[sum(x[i] for x in late)/len(late) for i in range(3)]
                ang = angle(cd['pre'],post) if cd['pre'] else 0
                score=0.30
                if cd['peak']>=c.impact+1.0: score+=0.10
                if cd['ff']: score+=0.25
                if ang>=45: score+=0.20
                elif ang>=25: score+=0.10
                if still: score+=0.25
                s.fall_cool=t+10000
                if still and score>=c.min_fall:
                    out.append(('fall',round(min(1,score),2),t)); s.fall_emit_t=t; s.inactive_done=False; s.last_moving=t
                s.cand=None
        return out
    def _inactive(s,t):
        if not s.inactive_done and s.fall_emit_t is not None:
            if t-s.last_moving>=25000:
                s.inactive_done=True; return [('inactivity',0.8,t)]
            if t-s.fall_emit_t>60000: s.inactive_done=True
        return []
    def _gait(s,t,dyn,gy,magG):
        out=[]
        # smoothing
        if s.last_t is None: dt=20
        else: dt=max(1,t-s.last_t)
        s.last_t=t
        alpha=(dt/1000)/((dt/1000)+1/(2*math.pi*6))
        s.sm = dyn if s.sm is None else s.sm+alpha*(dyn-s.sm)
        p2,p1=s.last_sm; s.last_sm=[p1,s.sm]
        if p1 is not None and p2 is not None:
            # p1 is previous sample value, candidate peak at previous sample
            if p1>p2 and p1>=s.sm:
                thr=max(1.0,0.5*s.peak_ema)
                if p1>thr and t-s.last_step>=220:
                    s.steps.append((t-dt,p1)); s.last_step=t
                    s.peak_ema = p1 if s.peak_ema==0 else s.peak_ema+0.3*(p1-s.peak_ema)
        if t-s.last_step>2000: s.peak_ema=0
        s.steps=[x for x in s.steps if t-x[0]<=3000]
        s.sm_win.append((t,s.sm)); s.sm_win=[x for x in s.sm_win if t-x[0]<=3000]
        s.dyn_win.append((t,dyn)); s.gyro_win.append((t,gy))
        s.dyn_win=[x for x in s.dyn_win if t-x[0]<=3000]; s.gyro_win=[x for x in s.gyro_win if t-x[0]<=3000]
        s.jerk.append((t,abs(magG-(s.prev_mag if hasattr(s,'prev_mag') else magG)))); s.prev_mag=magG
        s.jerk=[x for x in s.jerk if t-x[0]<=3000]
        if s.next_eval is None: s.next_eval=t+500
        if t>=s.next_eval:
            s.next_eval=t+500
            out+=s._evaluate(t)
        return out
    def _evaluate(s,t):
        c=s.c; out=[]
        n=len(s.steps)
        rms=math.sqrt(sum(d*d for _,d in s.dyn_win)/max(1,len(s.dyn_win)))
        grms=math.sqrt(sum(d*d for _,d in s.gyro_win)/max(1,len(s.gyro_win)))
        cad=0; cv=0
        if n>=4:
            span=(s.steps[-1][0]-s.steps[0][0])/1000
            cad=(n-1)/span if span>0 else 0
            iv=[(s.steps[i+1][0]-s.steps[i][0]) for i in range(n-1)]
            m=sum(iv)/len(iv); cv=math.sqrt(sum((x-m)**2 for x in iv)/len(iv))/m
        ac=s._periodicity()
        periodic = ac>=0.5
        if rms<0.5: lvl=0
        elif ac>=0.6 and n>=4 and cv<=0.35 and cad>=c.sprint_cad and rms>=c.sprint_rms: lvl=3
        elif periodic and n>=4 and cv<=0.35 and cad>=2.3 and rms>=3.0: lvl=2
        elif periodic and n>=3 and cv<=0.5 and cad>=1.0: lvl=1
        else: lvl=9
        # hysteresis
        if lvl==s.cand_level: s.cand_n+=1
        else: s.cand_level=lvl; s.cand_n=1
        prev=s.activity
        if s.cand_n>=2: s.activity=lvl
        s.hist.append((t,s.activity)); s.hist=[h for h in s.hist if t-h[0]<=20000]
        # sprint
        if s.activity==3 and prev!=3 and s.sprint_start is None:
            base=[rank(l) for tt,l in s.hist if t-15000<=tt<=t-2000]
            span_ok = s.hist[0][0] <= t-8000
            if span_ok and base and max(base)<=1 and t-s.sprint_cool>0 and t-s.struggle_cool>40000:
                s.sprint_start=t; s.sprint_rms=rms
        if s.sprint_start is not None:
            if t-s.sprint_start>=4000:
                seg=[rank(l) for tt,l in s.hist if tt>=s.sprint_start]
                if s.struggle_hits==0 and t-s.struggle_emit>=20000 and sum(1 for l in seg if l==3)>=0.7*len(seg):
                    conf=min(1,0.75+0.25*max(0,min(1,(rms-c.sprint_rms)/6)))
                    out.append(('sprint',round(conf,2),t)); s.sprint_cool=t+120000
                s.sprint_start=None
            elif rank(s.activity)<2 and t-s.sprint_start>1500:
                s.sprint_start=None
        # struggle
        irregular = (not periodic) or n<3 or cv>0.45
        big = grms>=c.gyro and rms>=c.acc_rms and irregular
        jk=sum(1 for _,j in s.jerk if j>1.0)
        if big and jk>=3: s.struggle_hits+=1
        else: s.struggle_hits=0
        if s.struggle_hits>=3 and t-s.struggle_cool>0:
            conf=min(1,0.5+0.25*max(0,min(1,(grms-c.gyro)/4))+0.25*max(0,min(1,(rms-c.acc_rms)/8)))
            out.append(('struggle',round(conf,2),t)); s.struggle_cool=t+60000; s.struggle_emit=t; s.struggle_hits=0
        return out

def _periodicity(s):
    w=[v for _,v in s.sm_win]
    n=len(w)
    if n<60: return 0.0
    m=sum(w)/n; x=[v-m for v in w]
    var=sum(v*v for v in x)
    if var<1e-6: return 0.0
    span=(s.sm_win[-1][0]-s.sm_win[0][0])/1000
    dt=span/(n-1)
    lo=max(1,int(0.2/dt)); hi=min(n//2,int(0.7/dt))
    best=0.0
    for lag in range(lo,hi+1):
        num=sum(x[i]*x[i+lag] for i in range(n-lag))
        den=math.sqrt(sum(v*v for v in x[:n-lag])*sum(v*v for v in x[lag:]))
        if den>0: best=max(best,num/den)
    return best
Engine._periodicity=_periodicity
