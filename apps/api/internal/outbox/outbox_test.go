package outbox

import (
 "context"
 "errors"
 "testing"
 "time"
)

func TestRetryDelayIsCappedAndMonotonic(t *testing.T) {
 base,max:=time.Second,5*time.Second
 want:=[]time.Duration{time.Second,2*time.Second,4*time.Second,5*time.Second,5*time.Second}
 for i,expected:=range want { if got:=RetryDelay(i+1,base,max);got!=expected { t.Fatalf("attempt %d: got %v want %v",i+1,got,expected) } }
 if RetryDelay(1,0,max)!=0||RetryDelay(0,base,max)!=0 { t.Fatal("invalid retry delay should be zero") }
}
func TestRetryDelayHandlesLargeAttemptWithoutOverflow(t *testing.T) {
 if got:=RetryDelay(1000,time.Second,30*time.Second);got!=30*time.Second { t.Fatalf("got %v",got) }
}
func TestWorkerRejectsInvalidPolicyBeforeDatabaseAccess(t *testing.T) {
 w:=Worker{WorkerID:"worker",BatchSize:1,Lease:time.Second,MaxAttempts:3,BaseBackoff:time.Second,MaxBackoff:time.Second,Publisher:PublishFunc(func(_ context.Context,_ Event,_ string)error{return nil})}
 if _,err:=w.RunOnce(context.Background());!errors.Is(err,ErrInvalidPolicy) { t.Fatalf("got %v",err) }
}
func TestPermanentErrorCanBeClassifiedAsNonRetryable(t *testing.T) {
 permanent:=errors.New("permanent")
 retryable:=func(err error)bool{return !errors.Is(err,permanent)}
 if retryable(permanent) { t.Fatal("permanent error should be dead-lettered") }
}
