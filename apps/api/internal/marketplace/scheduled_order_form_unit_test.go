package marketplace

import (
 "testing"
)

func TestCanonicalFormAnswers(t *testing.T) {
 cases:=[]struct{name,input,want string}{
  {name:"stable object ordering",input:`{"urgent":true,"rooms":2}`,want:`{"rooms":2,"urgent":true}`},
  {name:"whitespace normalization",input:`{ "rooms" : 2, "urgent" : true }`,want:`{"rooms":2,"urgent":true}`},
 }
 for _,tc:=range cases{t.Run(tc.name,func(t *testing.T){got,err:=canonicalFormAnswers([]byte(tc.input));if err!=nil{t.Fatal(err)};if string(got)!=tc.want{t.Fatalf("got %s want %s",got,tc.want)}})}
 for _,input:=range []string{`{"rooms":`,`{"rooms":2} {"rooms":3}`,`null trailing`}{
  if _,err:=canonicalFormAnswers([]byte(input));err==nil{t.Fatalf("invalid/trailing JSON accepted: %q",input)}
 }
 if got,err:=canonicalFormAnswers(nil);err!=nil||got!=nil{t.Fatalf("empty answers should remain absent: %q %v",got,err)}
}

func TestSameFormAnswersUsesSemanticJSON(t *testing.T) {
 stored:=`{"rooms": 2, "urgent": true}`
 if !sameFormAnswers(&stored,[]byte(`{ "urgent":true,"rooms":2 }`)){t.Fatal("equivalent JSONB answer objects must replay")}
 if sameFormAnswers(&stored,[]byte(`{"rooms":3,"urgent":true}`)){t.Fatal("changed answer object must conflict")}
 if !sameFormAnswers(nil,nil){t.Fatal("absent snapshots must match absent answers")}
 if sameFormAnswers(nil,[]byte(`{}`)){t.Fatal("new answers must not match an absent snapshot")}
}