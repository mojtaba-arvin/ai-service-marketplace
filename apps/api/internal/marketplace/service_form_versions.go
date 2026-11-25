package marketplace

import(
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "strings"
 "time"

 "github.com/jackc/pgx/v5"
 "github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
 "github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/serviceforms"
)

var(
 ErrServiceFormNotFound=errors.New("service form version not found")
 ErrInvalidServiceForm=errors.New("invalid service form version")
 ErrServiceFormNotPublished=errors.New("service form version is not publishable")
)
type PublishedServiceForm struct{
 ID string `json:"id"`
 MenuID string `json:"menu_id"`
 ServiceCode string `json:"service_code"`
 MarketCode string `json:"market_code"`
 Locale string `json:"locale"`
 Version int `json:"version"`
 Schema json.RawMessage `json:"schema"`
 PublishedAt time.Time `json:"published_at"`
}
func publicServiceForm(v ServiceFormVersion)PublishedServiceForm{return PublishedServiceForm{ID:v.ID,MenuID:v.MenuID,ServiceCode:v.ServiceCode,MarketCode:v.MarketCode,Locale:v.Locale,Version:v.Version,Schema:v.Schema,PublishedAt:*v.PublishedAt}}
type ServiceFormVersion struct{
 ID string `json:"id"`
 MenuID string `json:"menu_id"`
 ServiceCode string `json:"service_code"`
 MarketCode string `json:"market_code"`
 Locale string `json:"locale"`
 Version int `json:"version"`
 Schema json.RawMessage `json:"schema"`
 Status string `json:"status"`
 CreatedBy string `json:"created_by"`
 CreatedAt time.Time `json:"created_at"`
 PublishedBy *string `json:"published_by,omitempty"`
 PublishedAt *time.Time `json:"published_at,omitempty"`
 RetiredBy *string `json:"retired_by,omitempty"`
 RetiredAt *time.Time `json:"retired_at,omitempty"`
}
func authorizeCatalogMutationTx(ctx context.Context, tx pgx.Tx, actor Actor) error {
 if actor.TenantID != platformTenantID {
  return ErrForbidden
 }
 return authorizeOperatorTx(ctx, tx, actor, Resource{TenantID: platformTenantID}, ActionCatalogManage)
}
func decodeServiceFormSchema(raw []byte,serviceCode,locale string,version int)(serviceforms.Definition,error){
 if len(raw)==0||len(raw)>32*1024{return serviceforms.Definition{},ErrInvalidServiceForm}
 dec:=json.NewDecoder(bytes.NewReader(raw));dec.DisallowUnknownFields()
 var schema serviceforms.Schema
 if err:=dec.Decode(&schema);err!=nil{return serviceforms.Definition{},fmt.Errorf("%w: %v",ErrInvalidServiceForm,err)}
 if err:=dec.Decode(new(any));!errors.Is(err,io.EOF){return serviceforms.Definition{},ErrInvalidServiceForm}
 definition:=serviceforms.Definition{ServiceCode:serviceCode,Version:version,Locale:locale,Schema:schema}
 if err:=serviceforms.ValidateDefinition(definition);err!=nil{return serviceforms.Definition{},fmt.Errorf("%w: %v",ErrInvalidServiceForm,err)}
 return definition,nil
}
func scanServiceForm(row pgx.Row)(ServiceFormVersion,error){
 var v ServiceFormVersion
 err:=row.Scan(&v.ID,&v.MenuID,&v.ServiceCode,&v.MarketCode,&v.Locale,&v.Version,&v.Schema,&v.Status,&v.CreatedBy,&v.CreatedAt,&v.PublishedBy,&v.PublishedAt,&v.RetiredBy,&v.RetiredAt)
 return v,err
}
const serviceFormColumns=`f.id,f.menu_id,m.service_code,f.market_code,f.locale,f.version_no,f.schema,f.status,f.created_by,f.created_at,f.published_by,f.published_at,f.retired_by,f.retired_at`

func(s *Store)CreateServiceFormDraft(ctx context.Context,actor Actor,menuID,marketCode,locale string,schemaJSON []byte)(ServiceFormVersion,error){
 menuID=strings.TrimSpace(menuID);marketCode=strings.ToUpper(strings.TrimSpace(marketCode));locale=strings.TrimSpace(locale)
 if menuID==""||marketCode==""||locale==""||len(schemaJSON)==0||len(schemaJSON)>32*1024{return ServiceFormVersion{},ErrInvalidServiceForm}
 tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return ServiceFormVersion{},err};defer tx.Rollback(ctx)
 if err=authorizeCatalogMutationTx(ctx,tx,actor);err!=nil{return ServiceFormVersion{},err}
 var serviceCode string
 if err=tx.QueryRow(ctx,`SELECT service_code FROM service_price_menus WHERE id=$1 FOR UPDATE`,menuID).Scan(&serviceCode);errors.Is(err,pgx.ErrNoRows){return ServiceFormVersion{},ErrPriceMenuNotFound}else if err!=nil{return ServiceFormVersion{},err}
 var localeEnabled bool
 if err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM markets m JOIN market_locales l ON l.market_code=m.code WHERE m.code=$1 AND m.enabled=true AND l.locale=$2)`,marketCode,locale).Scan(&localeEnabled);err!=nil{return ServiceFormVersion{},err}
 if !localeEnabled{return ServiceFormVersion{},ErrMarketUnavailable}
 var version int
 if err=tx.QueryRow(ctx,`SELECT COALESCE(MAX(version_no),0)+1 FROM service_form_versions WHERE menu_id=$1 AND market_code=$2 AND locale=$3`,menuID,marketCode,locale).Scan(&version);err!=nil{return ServiceFormVersion{},err}
 definition,err:=decodeServiceFormSchema(schemaJSON,serviceCode,locale,version);if err!=nil{return ServiceFormVersion{},err}
 canonicalSchema,err:=json.Marshal(definition.Schema);if err!=nil{return ServiceFormVersion{},err}
 id,err:=newID();if err!=nil{return ServiceFormVersion{},err}
 _,err=tx.Exec(ctx,`
  INSERT INTO service_form_versions(id,menu_id,market_code,locale,version_no,schema,status,created_by)
  VALUES($1,$2,$3,$4,$5,$6::jsonb,'draft',$7)
 `,id,menuID,marketCode,locale,version,canonicalSchema,actor.SubjectID)
 if err!=nil{return ServiceFormVersion{},err}
 result,err:=scanServiceForm(tx.QueryRow(ctx,`SELECT `+serviceFormColumns+` FROM service_form_versions f JOIN service_price_menus m ON m.id=f.menu_id WHERE f.id=$1`,id))
 if err!=nil{return ServiceFormVersion{},err}
 if err=tx.Commit(ctx);err!=nil{return ServiceFormVersion{},err};return result,nil
}
func(s *Store)PublishServiceFormVersion(ctx context.Context,actor Actor,formID string)(ServiceFormVersion,error){
 formID=strings.TrimSpace(formID);if formID==""{return ServiceFormVersion{},ErrServiceFormNotFound}
 tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return ServiceFormVersion{},err};defer tx.Rollback(ctx)
 if err=authorizeCatalogMutationTx(ctx,tx,actor);err!=nil{return ServiceFormVersion{},err}
 var menuID string
 if err=tx.QueryRow(ctx,`SELECT menu_id FROM service_form_versions WHERE id=$1`,formID).Scan(&menuID);errors.Is(err,pgx.ErrNoRows){return ServiceFormVersion{},ErrServiceFormNotFound}else if err!=nil{return ServiceFormVersion{},err}
 var lockedMenuID string
 if err=tx.QueryRow(ctx,`SELECT id FROM service_price_menus WHERE id=$1 FOR UPDATE`,menuID).Scan(&lockedMenuID);err!=nil{return ServiceFormVersion{},err}
 target,err:=scanServiceForm(tx.QueryRow(ctx,`SELECT `+serviceFormColumns+` FROM service_form_versions f JOIN service_price_menus m ON m.id=f.menu_id WHERE f.id=$1 FOR UPDATE OF f`,formID))
 if errors.Is(err,pgx.ErrNoRows){return ServiceFormVersion{},ErrServiceFormNotFound};if err!=nil{return ServiceFormVersion{},err}
 if target.Status=="published"{if err=tx.Commit(ctx);err!=nil{return ServiceFormVersion{},err};return target,nil}
 if target.Status!="draft"{return ServiceFormVersion{},ErrServiceFormNotPublished}
 if _,err=tx.Exec(ctx,`UPDATE service_form_versions SET status='retired',retired_by=$4,retired_at=now() WHERE menu_id=$1 AND market_code=$2 AND locale=$3 AND status='published'`,target.MenuID,target.MarketCode,target.Locale,actor.SubjectID);err!=nil{return ServiceFormVersion{},err}
 if _,err=tx.Exec(ctx,`UPDATE service_form_versions SET status='published',published_by=$2,published_at=now() WHERE id=$1`,formID,actor.SubjectID);err!=nil{return ServiceFormVersion{},err}
 payload,_:=json.Marshal(map[string]any{"form_version_id":target.ID,"menu_id":target.MenuID,"service_code":target.ServiceCode,"market_code":target.MarketCode,"locale":target.Locale,"version":target.Version})
 eventID,err:=newID();if err!=nil{return ServiceFormVersion{},err}
 if _,err=outbox.AppendDomainEvent(ctx,tx,outbox.DomainEvent{ID:eventID,Type:"catalog.service_form.published",Version:1,OccurredAt:time.Now().UTC(),TenantID:platformTenantID,Aggregate:outbox.EventAggregate{Type:"service_form",ID:target.ID},Data:payload});err!=nil{return ServiceFormVersion{},err}
 published,err:=scanServiceForm(tx.QueryRow(ctx,`SELECT `+serviceFormColumns+` FROM service_form_versions f JOIN service_price_menus m ON m.id=f.menu_id WHERE f.id=$1`,formID))
 if err!=nil{return ServiceFormVersion{},err}
 if err=tx.Commit(ctx);err!=nil{return ServiceFormVersion{},err};return published,nil
}
func(s *Store)GetPublishedServiceForm(ctx context.Context,menuID,marketCode,locale string)(ServiceFormVersion,error){
 menuID=strings.TrimSpace(menuID);marketCode=strings.ToUpper(strings.TrimSpace(marketCode));locale=strings.TrimSpace(locale)
 if menuID==""||marketCode==""||locale==""{return ServiceFormVersion{},ErrServiceFormNotFound}
 v,err:=scanServiceForm(s.DB.QueryRow(ctx,`SELECT `+serviceFormColumns+` FROM service_form_versions f JOIN service_price_menus m ON m.id=f.menu_id WHERE f.menu_id=$1 AND f.market_code=$2 AND f.locale=$3 AND f.status='published'`,menuID,marketCode,locale))
 if errors.Is(err,pgx.ErrNoRows){return ServiceFormVersion{},ErrServiceFormNotFound};return v,err
}
