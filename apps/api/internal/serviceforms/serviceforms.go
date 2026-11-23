// Package serviceforms validates versioned, admin-defined service questionnaires.
package serviceforms

import (
 "bytes"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "math"
 "regexp"
 "strconv"
 "strings"
 "unicode/utf8"
)

var (
 ErrInvalidDefinition=errors.New("invalid service form definition")
 ErrInvalidAnswers=errors.New("service form answers do not match schema")
 serviceCodePattern=regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,79}$`)
 localePattern=regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)
)
type Definition struct {
 ServiceCode string `json:"service_code"`
 Version int `json:"version"`
 Locale string `json:"locale"`
 Schema Schema `json:"schema"`
}
type Schema struct {
 Type string `json:"type"`
 Title string `json:"title,omitempty"`
 Description string `json:"description,omitempty"`
 Required []string `json:"required,omitempty"`
 Properties map[string]Schema `json:"properties,omitempty"`
 AdditionalProperties *bool `json:"additionalProperties,omitempty"`
 Enum []any `json:"enum,omitempty"`
 MinLength *int `json:"minLength,omitempty"`
 MaxLength *int `json:"maxLength,omitempty"`
 Minimum *float64 `json:"minimum,omitempty"`
 Maximum *float64 `json:"maximum,omitempty"`
 Items *Schema `json:"items,omitempty"`
 MinItems *int `json:"minItems,omitempty"`
 MaxItems *int `json:"maxItems,omitempty"`
}
type FieldError struct{Path string;Code string}
func(e FieldError)Error()string{return fmt.Sprintf("%s: %s",e.Path,e.Code)}
func DefinitionFromJSON(raw []byte)(Definition,error){
 if len(raw)==0||len(raw)>32*1024{return Definition{},ErrInvalidDefinition}
 dec:=json.NewDecoder(bytes.NewReader(raw));dec.DisallowUnknownFields()
 var d Definition
 if err:=dec.Decode(&d);err!=nil{return Definition{},fmt.Errorf("%w: %v",ErrInvalidDefinition,err)}
 if err:=dec.Decode(new(any));!errors.Is(err,io.EOF){return Definition{},fmt.Errorf("%w: trailing content",ErrInvalidDefinition)}
 if err:=ValidateDefinition(d);err!=nil{return Definition{},err};return d,nil
}
func ValidateDefinition(d Definition)error{
 if !serviceCodePattern.MatchString(d.ServiceCode)||d.Version<1||!localePattern.MatchString(d.Locale){return ErrInvalidDefinition}
 fields:=0
 if err:=validateSchema(d.Schema,"$",0,&fields);err!=nil{return err}
 return nil
}
func validateSchema(s Schema,path string,depth int,fields *int)error{
 if depth>8{return FieldError{path,"schema nesting exceeds 8"}}
 switch s.Type{case "object","string","integer","number","boolean","array":default:return FieldError{path,"unsupported or missing type"}}
 if len(s.Title)>120||len(s.Description)>500{return FieldError{path,"title or description exceeds limit"}}
 if s.Type!="object"&&(len(s.Properties)>0||len(s.Required)>0||s.AdditionalProperties!=nil){return FieldError{path,"object keywords require object type"}}
 if s.Type!="array"&&(s.Items!=nil||s.MinItems!=nil||s.MaxItems!=nil){return FieldError{path,"array keywords require array type"}}
 if s.Type!="string"&&(s.MinLength!=nil||s.MaxLength!=nil){return FieldError{path,"string keywords require string type"}}
 if s.Type!="integer"&&s.Type!="number"&&(s.Minimum!=nil||s.Maximum!=nil){return FieldError{path,"numeric keywords require numeric type"}}
 if s.MinLength!=nil&&(*s.MinLength<0||*s.MinLength>4000){return FieldError{path,"invalid minLength"}}
 if s.MaxLength!=nil&&(*s.MaxLength<0||*s.MaxLength>4000){return FieldError{path,"invalid maxLength"}}
 if s.MinLength!=nil&&s.MaxLength!=nil&&*s.MinLength>*s.MaxLength{return FieldError{path,"minLength exceeds maxLength"}}
 if s.Minimum!=nil&&(math.IsNaN(*s.Minimum)||math.IsInf(*s.Minimum,0)){return FieldError{path,"invalid minimum"}}
 if s.Maximum!=nil&&(math.IsNaN(*s.Maximum)||math.IsInf(*s.Maximum,0)){return FieldError{path,"invalid maximum"}}
 if s.Minimum!=nil&&s.Maximum!=nil&&*s.Minimum>*s.Maximum{return FieldError{path,"minimum exceeds maximum"}}
 if s.MinItems!=nil&&(*s.MinItems<0||*s.MinItems>100){return FieldError{path,"invalid minItems"}}
 if s.MaxItems!=nil&&(*s.MaxItems<0||*s.MaxItems>100){return FieldError{path,"invalid maxItems"}}
 if s.MinItems!=nil&&s.MaxItems!=nil&&*s.MinItems>*s.MaxItems{return FieldError{path,"minItems exceeds maxItems"}}
 if len(s.Enum)>0&&len(s.Enum)>32{return FieldError{path,"enum exceeds 32 values"}}
 if s.Type=="object"{
  if len(s.Properties)==0||len(s.Properties)>64{return FieldError{path,"object must define 1 to 64 properties"}}
  if s.AdditionalProperties!=nil&&*s.AdditionalProperties{return FieldError{path,"additionalProperties=true is not supported; forms are closed by default"}}
  seen:=map[string]struct{}{}
  for _,name:=range s.Required{if _,ok:=s.Properties[name];!ok{return FieldError{path,"required field is not defined"}};if _,ok:=seen[name];ok{return FieldError{path,"duplicate required field"}};seen[name]=struct{}{}}
  for name,child:=range s.Properties{if name==""||len(name)>64||strings.TrimSpace(name)!=name{return FieldError{path,"invalid property name"}};*fields++;if *fields>128{return FieldError{path,"form exceeds 128 fields"}};if err:=validateSchema(child,path+"."+name,depth+1,fields);err!=nil{return err}}
 }
 if s.Type=="array"{if s.Items==nil{return FieldError{path,"array items schema is required"}};if err:=validateSchema(*s.Items,path+"[]",depth+1,fields);err!=nil{return err}}
 return nil
}
func ValidateAnswers(d Definition,raw []byte)error{
 if err:=ValidateDefinition(d);err!=nil{return err}
 if len(raw)==0||len(raw)>16*1024{return ErrInvalidAnswers}
 dec:=json.NewDecoder(bytes.NewReader(raw));dec.UseNumber()
 var value any
 if err:=dec.Decode(&value);err!=nil{return fmt.Errorf("%w: invalid JSON",ErrInvalidAnswers)}
 if err:=dec.Decode(new(any));!errors.Is(err,io.EOF){return fmt.Errorf("%w: trailing JSON",ErrInvalidAnswers)}
 if err:=validateValue(d.Schema,value,"$",0);err!=nil{return err};return nil
}
func validateValue(s Schema,v any,path string,depth int)error{
 if depth>8{return FieldError{path,"value nesting exceeds 8"}}
 if len(s.Enum)>0{matched:=false;for _,candidate:=range s.Enum{a,_:=json.Marshal(candidate);b,_:=json.Marshal(v);if bytes.Equal(a,b){matched=true;break}};if !matched{return FieldError{path,"value is not in enum"}}}
 switch s.Type{
 case "object":
  obj,ok:=v.(map[string]any);if !ok{return FieldError{path,"expected object"}}
  for _,name:=range s.Required{if _,exists:=obj[name];!exists{return FieldError{path+"."+name,"required field is missing"}}}
  for name,value:=range obj{child,exists:=s.Properties[name];if !exists{return FieldError{path+"."+name,"unknown field"}};if err:=validateValue(child,value,path+"."+name,depth+1);err!=nil{return err}}
 case "string":
  value,ok:=v.(string);if !ok{return FieldError{path,"expected string"}}
  length:=utf8.RuneCountInString(value);if s.MinLength!=nil&&length<*s.MinLength{return FieldError{path,"string is shorter than minLength"}};if s.MaxLength!=nil&&length>*s.MaxLength{return FieldError{path,"string exceeds maxLength"}}
 case "integer","number":
  n,ok:=v.(json.Number);if !ok{return FieldError{path,"expected number"}}
  var value float64;var err error
  if s.Type=="integer"{_,err=strconv.ParseInt(string(n),10,64);value,_=n.Float64()}else{value,err=n.Float64()}
  if err!=nil||math.IsNaN(value)||math.IsInf(value,0){return FieldError{path,"invalid numeric value"}}
  if s.Minimum!=nil&&value<*s.Minimum{return FieldError{path,"number is below minimum"}};if s.Maximum!=nil&&value>*s.Maximum{return FieldError{path,"number exceeds maximum"}}
 case "boolean":if _,ok:=v.(bool);!ok{return FieldError{path,"expected boolean"}}
 case "array":
  values,ok:=v.([]any);if !ok{return FieldError{path,"expected array"}}
  if s.MinItems!=nil&&len(values)<*s.MinItems{return FieldError{path,"array has fewer than minItems"}};if s.MaxItems!=nil&&len(values)>*s.MaxItems{return FieldError{path,"array exceeds maxItems"}}
  for i,item:=range values{if err:=validateValue(*s.Items,item,fmt.Sprintf("%s[%d]",path,i),depth+1);err!=nil{return err}}
 }
 return nil
}
