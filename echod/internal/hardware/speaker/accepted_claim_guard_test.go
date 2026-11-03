package speaker

import (
 "context"
 "path/filepath"
 "testing"
 "time"
 "github.com/HuskerMinion/techo5/echod/internal/config"
 "github.com/HuskerMinion/techo5/echod/internal/feature/luna"
)

// Reconstructed previous repro. Only the acquire call follows the updated
// production adapter: ClaimSpeech -> ClaimSpeechIf(generation.Err()==nil,...).
func TestCanceledAcquisitionMustNotPreemptNewPhone(t *testing.T) {
 d := NewDriver(&Player{})
 gate := make(chan struct{},1)
 entered, returned := make(chan struct{}), make(chan struct{})
 d.mu.Lock()
 out := luna.NewOutput(gate,func(generation context.Context,frames <-chan []int16) func(){
  close(entered)
  c := d.ClaimSpeechIf(func()bool{return generation.Err()==nil},"canceled Luna",func(ctx context.Context,p *Player)error{<-ctx.Done();return ctx.Err()})
  close(returned)
  return func(){d.CancelClaim(c)}
 })
 <-entered
 out.Reset()
 phoneCtx,phoneCancel:=context.WithCancel(context.Background())
 defer phoneCancel()
 phone:=&Claim{name:"new phone",cancel:phoneCancel,done:make(chan struct{})}
 go func(){<-phoneCtx.Done();close(phone.done)}()
 d.now=phone
 d.mu.Unlock()
 select {case <-returned:case <-time.After(2*time.Second):t.Fatal("acquisition failed to unwind")}
 gate<-struct{}{}; <-gate
 if phone.Stopped() || phone.Preempted() {
  t.Fatal("canceled Output acquisition displaced the newer phone before CancelClaim cleanup")
 }
}

type heldBackground struct { entered, release chan struct{} }
func (b heldBackground) Suspend(){close(b.entered); <-b.release}
func (b heldBackground) Resume(){}

func TestAcceptedThenCanceledAcquisitionMustNotDrainSuccessor(t *testing.T) {
 config.Use(filepath.Join(t.TempDir(),"state.json"))
 if err:=config.Set().Media().OnTurn(config.OnTurnPause);err!=nil{t.Fatal(err)}
 d:=NewDriver(&Player{})
 // An actual camera-over-music claim has not suspended the background.
 oldStarted:=make(chan struct{})
 old:=d.ClaimOver("camera",func(ctx context.Context,p *Player)error{close(oldStarted);<-ctx.Done();return ctx.Err()})
 <-oldStarted
 defer d.CancelClaim(old)
 bg:=heldBackground{make(chan struct{}),make(chan struct{})}
 d.Yields(bg)
 gate:=make(chan struct{},1)
 returned:=make(chan struct{})
 out:=luna.NewOutput(gate,func(generation context.Context,frames <-chan []int16)func(){
  c:=d.ClaimSpeechIf(func()bool{return generation.Err()==nil},"Luna",func(ctx context.Context,p *Player)error{<-ctx.Done();return ctx.Err()})
  close(returned)
  return func(){d.CancelClaim(c)}
 })
 <-bg.entered // guard accepted, d.now=Luna; Driver acquisition is in settle.
 out.Reset()  // no claim handle yet, so Reset returns immediately.
 // A real newer phone claim takes ownership. It waits the existing bounded
 // 1s for Luna, whose playback goroutine cannot run until acquire returns.
 phone:=d.ClaimCall("phone",func(ctx context.Context,p *Player)error{<-ctx.Done();return ctx.Err()})
 defer d.CancelClaim(phone)
 if d.Holder()!="phone"{t.Fatal("phone not owner")}
 d.p.mu.Lock();d.p.pending=make([]int16,1920);d.p.mu.Unlock()
 close(bg.release)
 select{case <-returned:case <-time.After(2*time.Second):t.Fatal("acquire did not unwind")}
 gate<-struct{}{}; <-gate // all late cancellation cleanup completed
 if d.Holder()!="phone" || phone.Stopped() {t.Fatal("phone unexpectedly stopped")}
 if n:=d.p.Queued();n!=960 {t.Fatalf("stale accepted acquisition drained successor phone: queued=%d, want960",n)}
}
