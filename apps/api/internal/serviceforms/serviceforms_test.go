package serviceforms

import (
 "errors"
 "testing"
)
func ptr[T any](v T)*T{return &v}
func validDefinition()Definition{return Definition{ServiceCode:"ac-repair",Version:2,Locale:"en",Schema:Schema{Type:"object",Required:[]string{"unit_model","rooms","urgent"},Properties:map[string]Schema{
 "unit_model":{Type:"string",MinLength:ptr(2),MaxLength:ptr(80)},
 "rooms":{Type:"integer",Minimum:ptr(1.0),Maximum:ptr(20.0)},
 "urgent":{Type:"boolean"},
 "symptoms":{Type:"array",MinItems:ptr(1),MaxItems:ptr(5),Items:&Schema{Type:"string",MaxLength:ptr(120)}},
}}}}
func TestValidateAnswersAcceptsTypedDynamicForm(t *testing.T){
 d:=validDefinition()
 if err:=ValidateAnswers(d,[]byte("{\"unit_model\":\"Split AC\",\"rooms\":2,\"urgent\":true,\"symptoms\":[\"not cooling\"]}"));err!=nil{t.Fatal(err)}
}
func TestValidateAnswersRejectsMissingUnknownAndWrongTypes(t *testing.T){
 d:=validDefinition()
 cases:=[][]byte{
  []byte("{\"rooms\":2,\"urgent\":true}"),
  []byte("{\"unit_model\":\"AC\",\"rooms\":2,\"urgent\":true,\"unexpected\":\"x\"}"),
  []byte("{\"unit_model\":\"AC\",\"rooms\":2.5,\"urgent\":true}"),
  []byte("{\"unit_model\":\"AC\",\"rooms\":2,\"urgent\":\"yes\"}"),
  []byte("{\"unit_model\":\"AC\",\"rooms\":2,\"urgent\":true,\"symptoms\":[]}"),
 }
 for _,raw:=range cases{if err:=ValidateAnswers(d,raw);!errors.Is(err,ErrInvalidAnswers){var field FieldError;if !errors.As(err,&field){t.Fatalf("expected validation error, got %v",err)}}}
}
func TestDefinitionRejectsUnsupportedAndUnsafeSchema(t *testing.T){
 d:=validDefinition();d.Schema.AdditionalProperties=ptr(true)
 if err:=ValidateDefinition(d);!errors.Is(err,ErrInvalidDefinition){var field FieldError;if !errors.As(err,&field){t.Fatalf("expected schema validation error, got %v",err)}}
 raw:=[]byte("{\"service_code\":\"ac-repair\",\"version\":1,\"locale\":\"en\",\"schema\":{\"type\":\"object\",\"properties\":{},\"patternProperties\":{}}}")
 if _,err:=DefinitionFromJSON(raw);!errors.Is(err,ErrInvalidDefinition){t.Fatalf("unsupported schema keyword accepted: %v",err)}
}
func TestDefinitionRejectsDuplicateRequiredAndInvalidBounds(t *testing.T){
 d:=validDefinition();d.Schema.Required=[]string{"unit_model","unit_model"}
 if err:=ValidateDefinition(d);err==nil{t.Fatal("duplicate required fields accepted")}
 d=validDefinition();d.Schema.Properties["rooms"]=Schema{Type:"integer",Minimum:ptr(5.0),Maximum:ptr(2.0)}
 if err:=ValidateDefinition(d);err==nil{t.Fatal("inverted numeric bounds accepted")}
}
