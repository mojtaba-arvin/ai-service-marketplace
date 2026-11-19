package marketplace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInspectionQuoteRequiresCustomerApprovalPostgres(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
	defer cancel()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil { t.Fatal(err) }
	defer pool.Close()
	for _,name:=range []string{"001_walking_skeleton.sql","002_identity.sql","004_catalog.sql","005_pricing.sql","006_provider_eligibility.sql","013_inspection_quotes.sql"} {
		migration,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
		if readErr!=nil { t.Fatal(readErr) }
		if _,err=pool.Exec(ctx,string(migration)); err!=nil { t.Fatalf("apply %s: %v",name,err) }
	}
	store:=&Store{DB:pool}
	identities:=make([]string,3)
	for i:=range identities {
		identities[i],err=newID()
		if err!=nil { t.Fatal(err) }
		phone:=fmt.Sprintf("+1995%011d",(time.Now().UnixNano()+int64(i))%100_000_000_000)
		if _,err=pool.Exec(ctx,`INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`,identities[i],phone); err!=nil { t.Fatal(err) }
	}
	customer:=Actor{SubjectID:identities[0],TenantID:"market-a",Role:RoleCustomer}
	professional:=Actor{SubjectID:identities[1],TenantID:"market-a",Role:RoleProfessional}
	operator:=Actor{SubjectID:identities[2],TenantID:"market-a",Role:RoleOperator}

	category,err:=store.CreateCategory(ctx,nil,fmt.Sprintf("repair-%d",time.Now().UnixNano()),"Repair")
	if err!=nil { t.Fatal(err) }
	menu,err:=store.CreatePriceMenu(ctx,category.ID,"inspection-repair")
	if err!=nil { t.Fatal(err) }
	if _,err=store.CreatePriceVersion(ctx,menu.ID,1000,"OMR"); err!=nil { t.Fatal(err) }
	order,created,err:=store.CreateOrderFromPrice(ctx,customer.SubjectID,"quote-order-"+fmt.Sprint(time.Now().UnixNano()),menu.ID)
	if err!=nil || !created { t.Fatalf("create priced order: %#v created=%v err=%v",order,created,err) }
	order,err=store.TransitionOrder(ctx,order.ID,"accepted")
	if err!=nil || order.Status!="accepted" { t.Fatalf("accept order: %#v %v",order,err) }

	provider,err:=store.CreateProviderProfile(ctx,professional,"Licensed repair professional")
	if err!=nil { t.Fatal(err) }
	if err=store.AddProviderSkill(ctx,professional,provider.ID,category.ID); err!=nil { t.Fatal(err) }
	if err=store.AddProviderServiceArea(ctx,professional,provider.ID,"OM","Muscat","Muscat","Al-Khuwair"); err!=nil { t.Fatal(err) }
	if err=store.VerifyProviderProfile(ctx,operator,provider.ID); err!=nil { t.Fatal(err) }
	if err=store.VerifyProviderSkill(ctx,operator,provider.ID,category.ID); err!=nil { t.Fatal(err) }
	if err=store.SetProviderActive(ctx,operator,provider.ID,true); err!=nil { t.Fatal(err) }

	expires:=time.Now().UTC().Add(time.Hour)
	quote,err:=store.CreateQuote(ctx,professional,order.ID,"Replace damaged socket and inspect wiring",expires,
		[]QuoteLineInput{
			{Code:"labor",Description:"Electrical labor",Quantity:2,UnitAmountMinor:1200},
			{Code:"parts",Description:"Replacement socket",Quantity:1,UnitAmountMinor:300},
		},
		[]QuoteEvidenceInput{{Kind:"photo",Reference:"evidence://inspection/socket-photo-1"}},
	)
	if err!=nil { t.Fatalf("create quote: %v",err) }
	if quote.TotalMinor!=2700 || quote.Currency!="OMR" || quote.RevisionNo!=1 || len(quote.Lines)!=2 || len(quote.Evidence)!=1 {
		t.Fatalf("unexpected quote snapshot: %#v",quote)
	}
	if _,err=store.ApproveQuote(ctx,professional,quote.ID); !errors.Is(err,ErrForbidden) {
		t.Fatalf("professional cannot approve customer quote: %v",err)
	}
	if _,err=store.AcceptQuote(ctx,professional,quote.ID); !errors.Is(err,ErrQuoteStateConflict) {
		t.Fatalf("provider cannot accept before customer approval: %v",err)
	}
	wrongCustomer:=Actor{SubjectID:"another-customer",TenantID:"market-a",Role:RoleCustomer}
	if _,err=store.ApproveQuote(ctx,wrongCustomer,quote.ID); !errors.Is(err,ErrForbidden) {
		t.Fatalf("another customer cannot approve quote: %v",err)
	}
	approved,err:=store.ApproveQuote(ctx,customer,quote.ID)
	if err!=nil || approved.Action!="approved" { t.Fatalf("customer approval: %#v %v",approved,err) }
	accepted,err:=store.AcceptQuote(ctx,professional,quote.ID)
	if err!=nil || accepted.Action!="accepted" { t.Fatalf("provider acceptance after approval: %#v %v",accepted,err) }
	if _,err=store.RejectQuote(ctx,customer,quote.ID,"changed mind"); !errors.Is(err,ErrQuoteStateConflict) {
		t.Fatalf("accepted quote cannot be rejected or rewritten: %v",err)
	}
	if _,err=store.CreateQuote(ctx,professional,order.ID,"second quote",time.Now().UTC().Add(time.Hour),
		[]QuoteLineInput{{Code:"labor",Description:"Labor",Quantity:1,UnitAmountMinor:100}},nil); !errors.Is(err,ErrQuoteStateConflict) {
		t.Fatalf("accepted quote prevents a competing revision: %v",err)
	}
	if _,err=store.CreateQuote(ctx,professional,order.ID,"expired",time.Now().UTC().Add(-time.Minute),
		[]QuoteLineInput{{Code:"labor",Description:"Labor",Quantity:1,UnitAmountMinor:100}},nil); !errors.Is(err,ErrInvalidQuote) {
		t.Fatalf("expired quote must be rejected: %v",err)
	}
	if _,err=pool.Exec(ctx,`UPDATE quote_line_items SET unit_amount_minor=1 WHERE quote_id=$1`,quote.ID); err==nil {
		t.Fatal("quote line items must be immutable")
	}
	if _,err=pool.Exec(ctx,`UPDATE inspection_quotes SET total_minor=1 WHERE id=$1`,quote.ID); err==nil {
		t.Fatal("quote revisions must be immutable")
	}
	if _,err=pool.Exec(ctx,`UPDATE quote_decisions SET reason='tampered' WHERE quote_id=$1`,quote.ID); err==nil {
		t.Fatal("quote decisions must be immutable")
	}
}
