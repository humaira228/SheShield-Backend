import math, random
from engine import *
random.seed(40)
HZ=50; DT=1000/HZ
def run(gen, dur_s, cfg=None):
    e=Engine(cfg or Cfg()); evs=[]
    n=int(dur_s*HZ)
    for i in range(n):
        t=i*DT
        a,g=gen(t/1000)
        evs+=e.process(t,a,g)
    return evs, e
def noise(s=0.02): return random.gauss(0,s*G)
def still_a(): return [noise(),noise(),G+noise()]
def gyro_n(): return [random.gauss(0,0.03) for _ in range(3)]
def gait(f, A, ph=0, jit=0.0):  # A in g amplitude of vertical component
    def fn(t):
        ph_t = t + jit*math.sin(2*math.pi*0.13*t)/(2*math.pi*0.13)
        fj = f
        v = A*G*(1+jit*2*math.sin(2*math.pi*0.31*t))*(math.sin(2*math.pi*fj*ph_t)+0.35*math.sin(4*math.pi*fj*ph_t+0.6))
        return [noise()+0.25*v, noise()+0.15*v, G+v+noise()], [random.gauss(0,0.4*f/2)+0.0 for _ in range(3)]
    return fn
def seq(*parts):  # (dur, fn)
    def fn(t):
        acc=0
        for d,f in parts:
            if t<acc+d: return f(t-acc)
            acc+=d
        return parts[-1][1](t-acc)
    return fn
still=lambda t:(still_a(),gyro_n())
walk=gait(1.9,0.22); jog=gait(2.7,0.65); sprint=gait(3.5,1.25)
def fall(freefall=True, tilt=True, get_up=None, impact=4.0):
    # standing (gravity along y) 0..2s ; fall; lying (gravity along z/x)
    def fn(t):
        if t<2.0: return [noise(),G+noise(),noise()], gyro_n()
        tt=t-2.0
        if freefall and tt<0.25: return [noise()*3,0.15*G+noise()*3,noise()*3],[random.gauss(0,1.2) for _ in range(3)]
        off=0.25 if freefall else 0.0
        if tt<off+0.06: return [impact*G*0.6,impact*G*0.7,impact*G*0.3],[random.gauss(0,3) for _ in range(3)]
        if tt<off+0.5: return [random.gauss(0,0.5*G),random.gauss(0,0.5*G),G+random.gauss(0,.3*G)],[random.gauss(0,1.5) for _ in range(3)]
        if get_up is not None and tt>get_up:
            return gait(1.6,0.2)(tt)
        if tilt: return [noise(),noise(),G+noise()],gyro_n()
        return [noise(),G+noise(),noise()],gyro_n()
    return fn
def struggle(t):
    return [random.gauss(0,0.9*G) , random.gauss(0,0.9*G), G+random.gauss(0,0.9*G)],[random.gauss(0,3.5) for _ in range(3)]
def drive(t):
    return [noise(.05),noise(.05),G+noise(.05)+0.1*G*math.sin(2*math.pi*1.1*t)*0], gyro_n()
def sit(t):
    if 3<t<3.3: return [0.1*G,G*1.8,0.05*G],[0.3,0.2,0.1]
    return still_a(),gyro_n()
def bump(t):
    if 3<t<3.05: return [0,G*2.9,0],[1,1,1]
    return [noise(),G+noise(),noise()],gyro_n()
def drop_and_pickup(t):
    # phone in hand upright, dropped onto table, picked up after 1s
    if t<2: return [noise(),G+noise(),noise()],gyro_n()
    tt=t-2
    if tt<0.3: return [0.1*G]*3,[random.gauss(0,3) for _ in range(3)]
    if tt<0.36: return [4.5*G*.5,4.5*G*.5,4.5*G*.3],[random.gauss(0,3) for _ in range(3)]
    if tt<1.2: return still_a(),gyro_n()
    return [random.gauss(0,0.4*G),random.gauss(0,0.4*G),G+random.gauss(0,.4*G)],[random.gauss(0,1.5) for _ in range(3)]

jsprint=gait(3.4,1.2,jit=0.08); jjog=gait(2.7,0.6,jit=0.08); jwalk=gait(1.8,0.25,jit=0.08)
cases=[
 ("jittery walk -> jittery sprint",seq((20,jwalk),(15,jsprint)),35,['sprint']),
 ("jittery jog 60",jjog,60,[]),
 ("jittery jog->jittery sprint",seq((40,jjog),(15,jsprint)),55,[]),

 ("still 30s",still,30,[]),
 ("walk 60s",walk,60,[]),
 ("jog 60s",jog,60,[]),
 ("steady sprint 60s (app start)",sprint,60,[]),
 ("walk 20s -> sprint 15s",seq((20,walk),(15,sprint)),35,['sprint']),
 ("still 20 -> sprint 15",seq((20,still),(15,sprint)),35,['sprint']),
 ("jog 40s -> sprint 15s",seq((40,jog),(15,sprint)),55,[]),
 ("walk 20 -> sprint 2s -> walk",seq((20,walk),(2,sprint),(20,walk)),42,[]),
 ("fall ff+tilt stays down 40s",fall(),42,['fall','inactivity']),
 ("fall no-ff tilt stays down",fall(freefall=False,impact=4.5),20,['fall']),
 ("fall then get up after 1.5s",fall(get_up=1.5),15,[]),
 ("fall + freefall lands same orientation (ambiguous: prompts)",fall(tilt=False),15,['fall']),
 ("stumble get up quickly",fall(get_up=0.8,impact=3.0),15,[]),
 ("sit down hard",sit,15,[]),
 ("car bump",bump,15,[]),
 ("drive vibration",drive,30,[]),
 ("phone drop + pickup",drop_and_pickup,15,[]),
 ("struggle 10s",seq((10,still),(10,struggle)),20,['struggle']),
 ("jog no struggle",jog,40,[]),
 ("walk no struggle",walk,40,[]),
]
bad=0
for name,fn,dur,exp in cases:
    evs,e=run(fn,dur)
    got=[x[0] for x in evs]
    ok = got==exp
    bad+= (not ok)
    print(("OK  " if ok else "FAIL"),name,"->",[(x[0],x[1],round(x[2]/1000,1)) for x in evs],"expected",exp)
print("failures",bad)
