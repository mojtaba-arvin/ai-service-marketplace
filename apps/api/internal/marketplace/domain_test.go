package marketplace

import (
	"errors"
	"testing"
)

func TestValidateAmount(t *testing.T) {
	cases:=[]struct{name,currency string; amount int64; wantErr error}{
		{"valid","OMR",1200,nil},
		{"zero","OMR",0,ErrInvalidAmount},
		{"negative","OMR",-1,ErrInvalidAmount},
		{"lowercase","omr",1200,ErrInvalidCurrency},
		{"wrong length","US",1200,ErrInvalidCurrency},
	}
	for _,tc:=range cases { t.Run(tc.name,func(t *testing.T){
		err:=ValidateAmount(tc.amount,tc.currency)
		if !errors.Is(err,tc.wantErr) { t.Fatalf("got %v, want %v",err,tc.wantErr) }
	})}
}
func TestOrderTransitions(t *testing.T) {
	valid:=[][2]string{{"requested","accepted"},{"requested","cancelled"},{"accepted","in_progress"},{"in_progress","completed"},{"in_progress","disputed"}}
	for _,p:=range valid { if !CanTransition(p[0],p[1]) { t.Errorf("expected %s -> %s",p[0],p[1]) } }
	invalid:=[][2]string{{"requested","completed"},{"completed","accepted"},{"cancelled","requested"},{"disputed","completed"}}
	for _,p:=range invalid { if CanTransition(p[0],p[1]) { t.Errorf("unexpected %s -> %s",p[0],p[1]) } }
}
