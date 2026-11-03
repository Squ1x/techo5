package nearest

import (
 "io"
 "log/slog"
 "sync"
 "testing"
 "time"
 "github.com/HuskerMinion/techo5/echod/internal/feature/voice"
)

func TestReviewSnapshotRoutingDuringAdvertisedReload(t *testing.T) {
 oldLog:=slog.Default();slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard,nil)));defer slog.SetDefault(oldLog)
 // Real singleton and real advertised getter/setter; no services started or hardware I/O.
 n:=Get()
 n.mu.Lock();n.enabled=false;n.mu.Unlock()
 oldNow,oldWoke:=n.wokeNow,n.woke;n.wokeNow=func(int,...string){};n.woke=func(int,time.Time,...string){};defer func(){n.wokeNow,n.woke=oldNow,oldWoke}()
 v:=voice.Get()
 old:=v.ActiveWakeWords();defer v.SetActiveWakeWords(old)
 v.SetActiveWakeWords([]string{"hey_odin"})
 for _,tc:=range []struct{name string; detect func()}{
 {"immediate",func(){n.Woke(0,"galileo")}},
 {"gated",func(){n.WokeGated(0,time.Now(),func(open func()){open()},"galileo")}},
 }{t.Run(tc.name,func(t *testing.T){
 start:=make(chan struct{})
 var wg sync.WaitGroup
 wg.Add(2)
 go func(){defer wg.Done();<-start;for i:=0;i<1000;i++{v.SetActiveWakeWords([]string{"hey_odin"})}}()
 go func(){defer wg.Done();<-start;for i:=0;i<1000;i++{tc.detect()}}()
 close(start);wg.Wait()
 })}
}
