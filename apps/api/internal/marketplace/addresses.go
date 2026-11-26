package marketplace

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/adapters"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/marketconfig"
)

var (
	ErrAddressNotFound = errors.New("saved address not found")
	ErrAddressReferenced = errors.New("saved address is referenced by order history")
	ErrInvalidAddress = errors.New("invalid saved address")
	ErrAddressForbidden = errors.New("saved address access denied")
)

type SavedAddressInput struct {
	Label string
	AddressLine1 string
	AddressLine2 string
	Locality string
	Region string
	PostalCode string
	CountryCode string
}

type SavedAddress struct {
	ID string `json:"id"`
	Label string `json:"label"`
	AddressLine1 string `json:"address_line1"`
	AddressLine2 string `json:"address_line2,omitempty"`
	Locality string `json:"locality"`
	Region string `json:"region,omitempty"`
	PostalCode string `json:"postal_code,omitempty"`
	CountryCode string `json:"country_code"`
	FormattedAddress string `json:"formatted_address"`
	Latitude *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`
	Geocoder string `json:"geocoder,omitempty"`
	GeocoderVersion string `json:"geocoder_version,omitempty"`
	IsDefault bool `json:"is_default"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func validateSavedAddress(in SavedAddressInput) error {
	fields:=[]struct{value string;max int;required bool}{
		{in.Label,80,true},{in.AddressLine1,200,true},{in.AddressLine2,200,false},
		{in.Locality,120,true},{in.Region,120,false},{in.PostalCode,32,false},{in.CountryCode,2,true},
	}
	for _,field:=range fields {
		if strings.TrimSpace(field.value)!=field.value || len(field.value)>field.max || (field.required&&field.value=="") {return ErrInvalidAddress}
	}
	if len(in.CountryCode)!=2 || strings.ToUpper(in.CountryCode)!=in.CountryCode {return ErrInvalidAddress}
	for _,r:=range in.CountryCode {if r<'A'||r>'Z'{return ErrInvalidAddress}}
	return nil
}

func validateAddressScope(registry *marketconfig.Registry, marketID, locale string) error {
	if registry==nil { return marketconfig.ErrUnknownMarket }
	_,err:=registry.Resolve(marketID,locale)
	return err
}

func validateAddressActor(actor Actor) error {
	if actor.Role!=RoleCustomer || strings.TrimSpace(actor.SubjectID)=="" || strings.TrimSpace(actor.TenantID)=="" {return ErrAddressForbidden}
	return nil
}

func validateGeocodeResult(result *adapters.GeocodeResult) error {
	if result==nil{return nil}
	if strings.TrimSpace(result.FormattedAddress)=="" || result.Latitude < -90 || result.Latitude > 90 ||
		result.Longitude < -180 || result.Longitude > 180 || math.IsNaN(result.Latitude) || math.IsNaN(result.Longitude) ||
		math.IsInf(result.Latitude,0) || math.IsInf(result.Longitude,0) || result.Provider=="" || result.ProviderVersion=="" {
		return ErrInvalidAddress
	}
	return nil
}

func GeocodeSavedAddress(ctx context.Context, matrix *marketconfig.Matrix, marketID, locale string, runner adapters.Runner, geocoder adapters.Geocoder, address, idempotencyKey string) (adapters.GeocodeResult,error) {
	if matrix==nil{return adapters.GeocodeResult{},marketconfig.ErrUnknownMarket}
	if err:=matrix.Require(marketID,locale,"maps","geocode");err!=nil{return adapters.GeocodeResult{},err}
	return adapters.Geocode(ctx,runner,geocoder,adapters.GeocodeRequest{MarketID:marketID,Locale:locale,Address:address,IdempotencyKey:idempotencyKey})
}

func (s *Store) CreateSavedAddress(ctx context.Context, actor Actor, registry *marketconfig.Registry, marketID, locale string, input SavedAddressInput, geocoded *adapters.GeocodeResult) (SavedAddress,error) {
	if err:=validateAddressActor(actor);err!=nil{return SavedAddress{},err}
	if err:=validateAddressScope(registry,marketID,locale);err!=nil{return SavedAddress{},err}
	if err:=validateSavedAddress(input);err!=nil{return SavedAddress{},err}
	if err:=validateGeocodeResult(geocoded);err!=nil{return SavedAddress{},err}
	id,err:=newID();if err!=nil{return SavedAddress{},err}
	formatted:=strings.Join([]string{input.AddressLine1,input.Locality,input.Region,input.PostalCode,input.CountryCode},", ")
	var latitude,longitude any
	var provider,version string
	if geocoded!=nil {formatted=geocoded.FormattedAddress;latitude,longitude=geocoded.Latitude,geocoded.Longitude;provider,version=geocoded.Provider,geocoded.ProviderVersion}
	var out SavedAddress
	err=s.DB.QueryRow(ctx,`
		INSERT INTO customer_addresses(id,identity_id,tenant_id,market_code,locale,label,address_line1,address_line2,locality,region,postal_code,country_code,formatted_address,latitude,longitude,geocoder,geocoder_version)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		RETURNING id,label,address_line1,address_line2,locality,region,postal_code,country_code,formatted_address,latitude,longitude,geocoder,geocoder_version,is_default,created_at::text,updated_at::text
	`,id,actor.SubjectID,actor.TenantID,marketID,locale,input.Label,input.AddressLine1,input.AddressLine2,input.Locality,input.Region,input.PostalCode,input.CountryCode,formatted,latitude,longitude,provider,version).
		Scan(&out.ID,&out.Label,&out.AddressLine1,&out.AddressLine2,&out.Locality,&out.Region,&out.PostalCode,&out.CountryCode,&out.FormattedAddress,&out.Latitude,&out.Longitude,&out.Geocoder,&out.GeocoderVersion,&out.IsDefault,&out.CreatedAt,&out.UpdatedAt)
	if err!=nil{return SavedAddress{},fmt.Errorf("create saved address: %w",err)}
	return out,nil
}

func (s *Store) ListSavedAddresses(ctx context.Context, actor Actor, registry *marketconfig.Registry, marketID, locale string) ([]SavedAddress,error) {
	if err:=validateAddressActor(actor);err!=nil{return nil,err}
	if err:=validateAddressScope(registry,marketID,locale);err!=nil{return nil,err}
	rows,err:=s.DB.Query(ctx,`
		SELECT id,label,address_line1,address_line2,locality,region,postal_code,country_code,formatted_address,latitude,longitude,geocoder,geocoder_version,is_default,created_at::text,updated_at::text
		FROM customer_addresses WHERE identity_id=$1 AND tenant_id=$2 AND market_code=$3 AND locale=$4
		ORDER BY is_default DESC,created_at,id
	`,actor.SubjectID,actor.TenantID,marketID,locale)
	if err!=nil{return nil,err}
	defer rows.Close()
	result:=make([]SavedAddress,0)
	for rows.Next(){var item SavedAddress
		if err:=rows.Scan(&item.ID,&item.Label,&item.AddressLine1,&item.AddressLine2,&item.Locality,&item.Region,&item.PostalCode,&item.CountryCode,&item.FormattedAddress,&item.Latitude,&item.Longitude,&item.Geocoder,&item.GeocoderVersion,&item.IsDefault,&item.CreatedAt,&item.UpdatedAt);err!=nil{return nil,err}
		result=append(result,item)
	}
	return result,rows.Err()
}

func (s *Store) UpdateSavedAddress(ctx context.Context, actor Actor, registry *marketconfig.Registry, marketID, locale, addressID string, input SavedAddressInput, geocoded *adapters.GeocodeResult) (SavedAddress,error) {
	if err:=validateAddressActor(actor);err!=nil{return SavedAddress{},err}
	if err:=validateAddressScope(registry,marketID,locale);err!=nil{return SavedAddress{},err}
	if err:=validateSavedAddress(input);err!=nil{return SavedAddress{},err}
	if err:=validateGeocodeResult(geocoded);err!=nil{return SavedAddress{},err}
	formatted:=strings.Join([]string{input.AddressLine1,input.Locality,input.Region,input.PostalCode,input.CountryCode},", ")
	var latitude,longitude,provider,version any
	if geocoded!=nil {formatted=geocoded.FormattedAddress;latitude=geocoded.Latitude;longitude=geocoded.Longitude;provider=geocoded.Provider;version=geocoded.ProviderVersion}
	var out SavedAddress
	err:=s.DB.QueryRow(ctx,`
		UPDATE customer_addresses SET label=$6,address_line1=$7,address_line2=$8,locality=$9,region=$10,postal_code=$11,country_code=$12,
		formatted_address=$13,latitude=$14,longitude=$15,geocoder=$16,
		geocoder_version=$17,updated_at=now()
		WHERE id=$1 AND identity_id=$2 AND tenant_id=$3 AND market_code=$4 AND locale=$5
		RETURNING id,label,address_line1,address_line2,locality,region,postal_code,country_code,formatted_address,latitude,longitude,geocoder,geocoder_version,is_default,created_at::text,updated_at::text
	`,addressID,actor.SubjectID,actor.TenantID,marketID,locale,input.Label,input.AddressLine1,input.AddressLine2,input.Locality,input.Region,input.PostalCode,input.CountryCode,formatted,latitude,longitude,provider,version).
		Scan(&out.ID,&out.Label,&out.AddressLine1,&out.AddressLine2,&out.Locality,&out.Region,&out.PostalCode,&out.CountryCode,&out.FormattedAddress,&out.Latitude,&out.Longitude,&out.Geocoder,&out.GeocoderVersion,&out.IsDefault,&out.CreatedAt,&out.UpdatedAt)
	if errors.Is(err,pgx.ErrNoRows){return SavedAddress{},ErrAddressNotFound}
	return out,err
}

func (s *Store) SetDefaultSavedAddress(ctx context.Context, actor Actor, registry *marketconfig.Registry, marketID, locale, addressID string) error {
	if err:=validateAddressActor(actor);err!=nil{return err}
	if err:=validateAddressScope(registry,marketID,locale);err!=nil{return err}
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return err};defer tx.Rollback(ctx)
	lockKey:=actor.SubjectID+":"+actor.TenantID+":"+marketID
	if _,err=tx.Exec(ctx,`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,lockKey);err!=nil{return err}
	var found string
	err=tx.QueryRow(ctx,`SELECT id FROM customer_addresses WHERE id=$1 AND identity_id=$2 AND tenant_id=$3 AND market_code=$4 AND locale=$5 FOR UPDATE`,addressID,actor.SubjectID,actor.TenantID,marketID,locale).Scan(&found)
	if errors.Is(err,pgx.ErrNoRows){return ErrAddressNotFound};if err!=nil{return err}
	if _,err=tx.Exec(ctx,`UPDATE customer_addresses SET is_default=false,updated_at=now() WHERE identity_id=$1 AND tenant_id=$2 AND market_code=$3 AND is_default=true`,actor.SubjectID,actor.TenantID,marketID);err!=nil{return err}
	if _,err=tx.Exec(ctx,`UPDATE customer_addresses SET is_default=true,updated_at=now() WHERE id=$1`,addressID);err!=nil{return err}
	return tx.Commit(ctx)
}

func (s *Store) DeleteSavedAddress(ctx context.Context, actor Actor, registry *marketconfig.Registry, marketID, locale, addressID string) error {
	if err:=validateAddressActor(actor);err!=nil{return err}
	if err:=validateAddressScope(registry,marketID,locale);err!=nil{return err}
	tag,err:=s.DB.Exec(ctx,`DELETE FROM customer_addresses WHERE id=$1 AND identity_id=$2 AND tenant_id=$3 AND market_code=$4 AND locale=$5`,addressID,actor.SubjectID,actor.TenantID,marketID,locale)
	if err!=nil {
		var pgErr *pgconn.PgError
		if errors.As(err,&pgErr)&&pgErr.Code=="23503"{return ErrAddressReferenced}
		return err
	}
	if tag.RowsAffected()!=1{return ErrAddressNotFound}
	return nil
}
