package marketplace

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

type captureOTPDelivery struct {
    phone string
    code string
    key string
}

func (d *captureOTPDelivery) SendOTP(_ context.Context, phone, code, key string) error {
    d.phone,d.code,d.key=phone,code,key
    return nil
}

func TestMVPHTTPVerticalSlicePostgres(t *testing.T) {
    dsn := os.Getenv("TEST_DATABASE_URL")
    if dsn == "" {
        t.Skip("TEST_DATABASE_URL is not configured")
    }
    ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
    defer cancel()
    pool, err := pgxpool.New(ctx, dsn)
    if err != nil {
        t.Fatal(err)
    }
    defer pool.Close()

    migrations := []string{
        "001_walking_skeleton.sql","002_identity.sql","003_sessions.sql","004_catalog.sql",
        "005_pricing.sql","006_provider_eligibility.sql","007_market_config.sql","008_catalog_localization.sql",
        "009_ledger_accounts.sql","010_ledger_idempotency_reversal.sql","011_payment_adapter_contract.sql",
        "012_settlement_reconciliation.sql","013_inspection_quotes.sql","014_quote_variations.sql",
        "015_order_schedule.sql","016_cancellation_policy.sql","017_provider_availability.sql",
        "018_dispatch_offers.sql","019_outbox_delivery.sql","020_professional_verification.sql",
        "021_support_cases.sql","022_identity_memberships.sql",
    }
    for _, name := range migrations {
        raw, readErr := os.ReadFile(filepath.Join("..","..","migrations",name))
        if readErr != nil {
            t.Fatal(readErr)
        }
        if _, err = pool.Exec(ctx,string(raw)); err != nil {
            t.Fatalf("apply %s: %v",name,err)
        }
    }

    suffix := fmt.Sprint(time.Now().UnixNano())
    tenantID := "mvp-test-" + suffix
    marketCode := "ZZ"
    operatorPhone := "+1996" + suffix[len(suffix)-11:]
    professionalPhone := "+1997" + suffix[len(suffix)-11:]
    customerPhone := "+1998" + suffix[len(suffix)-11:]
    otherCustomerPhone := "+1999" + suffix[len(suffix)-11:]
    for _, phone := range []string{operatorPhone,professionalPhone,customerPhone,otherCustomerPhone} {
        if len(phone) > 16 {
            t.Fatalf("test phone is too long: %s",phone)
        }
    }

    store := &Store{DB:pool}
    if _, err = pool.Exec(ctx,`
        INSERT INTO markets(code,default_locale,currency,timezone,policy_version,enabled)
        VALUES($1,'en','USD','UTC','mvp-test-1',true)
        ON CONFLICT(code) DO UPDATE SET enabled=true,currency='USD',timezone='UTC'
    `,marketCode); err != nil {
        t.Fatal(err)
    }
    if _, err = pool.Exec(ctx,`
        INSERT INTO market_locales(market_code,locale,is_default)
        VALUES($1,'en',true) ON CONFLICT(market_code,locale) DO NOTHING
    `,marketCode); err != nil {
        t.Fatal(err)
    }

    identities := make(map[string]string)
    for name, phone := range map[string]string{
        "operator":operatorPhone,"professional":professionalPhone,
        "customer":customerPhone,"other":otherCustomerPhone,
    } {
        id, idErr := newID()
        if idErr != nil {
            t.Fatal(idErr)
        }
        if _, err = pool.Exec(ctx,`INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`,id,phone); err != nil {
            t.Fatal(err)
        }
        identities[name] = id
    }

    operator := Actor{SubjectID:identities["operator"],TenantID:tenantID,Role:RoleOperator}
    professional := Actor{SubjectID:identities["professional"],TenantID:tenantID,Role:RoleProfessional}
    customer := Actor{SubjectID:identities["customer"],TenantID:tenantID,Role:RoleCustomer}
    otherCustomer := Actor{SubjectID:identities["other"],TenantID:tenantID,Role:RoleCustomer}
    for _, actor := range []Actor{operator,professional,customer,otherCustomer} {
        if err = store.EnsureIdentityMembership(ctx,actor.SubjectID,tenantID,actor.Role); err != nil {
            t.Fatal(err)
        }
    }

    category, err := store.CreateCategory(ctx,nil,"mvp-test-"+suffix,"MVP Test Service")
    if err != nil {
        t.Fatal(err)
    }
    if _, err = pool.Exec(ctx,`
        INSERT INTO market_category_visibility(market_code,category_id,visible)
        VALUES($1,$2,true)
    `,marketCode,category.ID); err != nil {
        t.Fatal(err)
    }
    menu, err := store.CreatePriceMenu(ctx,category.ID,"standard-service")
    if err != nil {
        t.Fatal(err)
    }
    if _, err = store.CreatePriceVersion(ctx,menu.ID,4500,"USD"); err != nil {
        t.Fatal(err)
    }

    profile, err := store.CreateProviderProfile(ctx,professional,"MVP Test Professional")
    if err != nil {
        t.Fatal(err)
    }
    if err = store.AddProviderSkill(ctx,professional,profile.ID,category.ID); err != nil {
        t.Fatal(err)
    }
    if err = store.AddProviderServiceArea(ctx,professional,profile.ID,marketCode,"Test Region","Test City","Test Area"); err != nil {
        t.Fatal(err)
    }
    if err = store.VerifyProviderProfile(ctx,operator,profile.ID); err != nil {
        t.Fatal(err)
    }
    if err = store.VerifyProviderSkill(ctx,operator,profile.ID,category.ID); err != nil {
        t.Fatal(err)
    }
    if err = store.SetProviderActive(ctx,operator,profile.ID,true); err != nil {
        t.Fatal(err)
    }
    now := time.Now().UTC()
    if _, err = store.SetProviderAvailability(ctx,professional,profile.ID,"available",
        now.Add(-time.Hour),now.Add(12*time.Hour),"UTC"); err != nil {
        t.Fatal(err)
    }

    // Seed a zero-fee cancellation policy. Cancellation records an outcome only;
    // it does not move funds or create a payment ledger posting.
    for _, status := range []string{"requested","accepted"} {
        if _, err = pool.Exec(ctx,`
            INSERT INTO cancellation_policies(
                id,market_code,policy_version,actor_role,from_status,cutoff_minutes,
                fee_basis_points,effective_from,created_by_identity_id
            ) VALUES($1,$2,'mvp-test-1','customer',$3,0,0,now()-interval '1 hour',$4)
            ON CONFLICT(market_code,policy_version,actor_role,from_status) DO NOTHING
        `,"cancel-"+status+"-"+suffix,marketCode,status,operator.SubjectID); err != nil {
            t.Fatal(err)
        }
    }

    pepper := []byte("0123456789abcdef0123456789abcdef")
    delivery := &captureOTPDelivery{}
    handler := &Handler{
        Store:store,OTPPepper:pepper,SessionPepper:pepper,OTPDelivery:delivery,
        AccessTTL:15*time.Minute,RefreshTTL:24*time.Hour,
        DefaultTenantID:tenantID,DefaultMarketCode:marketCode,BootstrapOperatorPhone:operatorPhone,
    }
    tokens := make(map[string]string)
    call := func(method,path,token,key string,payload any) (*httptest.ResponseRecorder,map[string]any) {
        var body bytes.Buffer
        if payload != nil {
            if encodeErr := json.NewEncoder(&body).Encode(payload); encodeErr != nil {
                t.Fatal(encodeErr)
            }
        }
        req := httptest.NewRequest(method,path,&body)
        if token != "" {
            req.Header.Set("Authorization","Bearer "+token)
        }
        if key != "" {
            req.Header.Set("Idempotency-Key",key)
        }
        rec := httptest.NewRecorder()
        handler.ServeHTTP(rec,req)
        var decoded map[string]any
        _ = json.Unmarshal(rec.Body.Bytes(),&decoded)
        return rec,decoded
    }

    otpRequest,otpRequestBody := call(http.MethodPost,"/v1/auth/otp/request","", "",map[string]string{"phone":operatorPhone})
    if otpRequest.Code != http.StatusAccepted || delivery.code == "" || delivery.key == "" {
        t.Fatalf("operator OTP request: status=%d body=%s",otpRequest.Code,otpRequest.Body.String())
    }
    otpVerify,otpVerifyBody := call(http.MethodPost,"/v1/auth/otp/verify","", "",map[string]string{
        "challenge_id":otpRequestBody["challenge_id"].(string),"code":delivery.code,
    })
    if otpVerify.Code != http.StatusOK {
        t.Fatalf("operator OTP verification: status=%d body=%s",otpVerify.Code,otpVerify.Body.String())
    }
    tokens["operator"] = otpVerifyBody["access_token"].(string)
    meResponse,meBody := call(http.MethodGet,"/v1/auth/me",tokens["operator"],"",nil)
    if meResponse.Code != http.StatusOK {
        t.Fatalf("operator identity endpoint: status=%d body=%s",meResponse.Code,meResponse.Body.String())
    }
    roles := meBody["roles"].([]any)
    hasOperatorRole := false
    for _, role := range roles {
        if role == string(RoleOperator) { hasOperatorRole=true }
    }
    if !hasOperatorRole { t.Fatalf("bootstrap operator role was not provisioned: %#v",roles) }

    for name, phone := range map[string]string{
        "professional":professionalPhone,"customer":customerPhone,"other":otherCustomerPhone,
    } {
        session, issueErr := store.IssueSession(ctx,VerifiedIdentity{phone:phone},pepper,15*time.Minute,24*time.Hour)
        if issueErr != nil { t.Fatal(issueErr) }
        tokens[name] = session.AccessToken
    }

    catalogResponse,catalog := call(http.MethodGet,"/v1/catalog","","",nil)
    if catalogResponse.Code != http.StatusOK {
        t.Fatalf("catalog: status=%d body=%s",catalogResponse.Code,catalogResponse.Body.String())
    }
    catalogItems,ok := catalog["items"].([]any)
    if !ok || len(catalogItems)!=1 {
        t.Fatalf("expected one visible catalog item, got %#v",catalog)
    }

    scheduleOne := time.Now().UTC().Add(2*time.Hour).Truncate(time.Microsecond)
    orderInput := map[string]any{
        "menu_id":menu.ID,"scheduled_at":scheduleOne.Format(time.RFC3339Nano),
        "country_code":marketCode,"region_code":"Test Region","city_code":"Test City","area_code":"Test Area",
    }
    orderResponse,orderBody := call(http.MethodPost,"/v1/orders",tokens["customer"],"mvp-order-1-"+suffix,orderInput)
    if orderResponse.Code != http.StatusCreated {
        t.Fatalf("create order: status=%d body=%s",orderResponse.Code,orderResponse.Body.String())
    }
    order := orderBody["order"].(map[string]any)
    orderID := order["id"].(string)

    replayResponse,replayBody := call(http.MethodPost,"/v1/orders",tokens["customer"],"mvp-order-1-"+suffix,orderInput)
    if replayResponse.Code != http.StatusOK || replayBody["created"] != false ||
        replayBody["order"].(map[string]any)["id"] != orderID {
        t.Fatalf("idempotent replay failed: status=%d body=%s",replayResponse.Code,replayResponse.Body.String())
    }

    forbiddenResponse,_ := call(http.MethodPost,"/v1/operator/orders/"+orderID+"/dispatch",tokens["customer"],"",nil)
    if forbiddenResponse.Code != http.StatusForbidden {
        t.Fatalf("customer must not dispatch orders: status=%d body=%s",forbiddenResponse.Code,forbiddenResponse.Body.String())
    }

    dispatchResponse,dispatchBody := call(http.MethodPost,"/v1/operator/orders/"+orderID+"/dispatch",tokens["operator"],"",nil)
    if dispatchResponse.Code != http.StatusCreated {
        t.Fatalf("dispatch: status=%d body=%s",dispatchResponse.Code,dispatchResponse.Body.String())
    }
    offers := dispatchBody["offers"].([]any)
    if len(offers)!=1 {
        t.Fatalf("expected one provider offer, got %#v",offers)
    }
    offerID := offers[0].(map[string]any)["id"].(string)

    offersResponse,offersBody := call(http.MethodGet,"/v1/professional/offers",tokens["professional"],"",nil)
    if offersResponse.Code != http.StatusOK || len(offersBody["offers"].([]any))!=1 {
        t.Fatalf("professional offers: status=%d body=%s",offersResponse.Code,offersResponse.Body.String())
    }

    otherAccept,_ := call(http.MethodPost,"/v1/professional/offers/"+offerID+"/accept",tokens["other"],"",nil)
    if otherAccept.Code != http.StatusForbidden {
        t.Fatalf("unassigned professional must not accept offer: status=%d body=%s",otherAccept.Code,otherAccept.Body.String())
    }
    acceptResponse,acceptBody := call(http.MethodPost,"/v1/professional/offers/"+offerID+"/accept",tokens["professional"],"",nil)
    if acceptResponse.Code != http.StatusOK {
        t.Fatalf("accept offer: status=%d body=%s",acceptResponse.Code,acceptResponse.Body.String())
    }
    if acceptBody["assignment"] == nil {
        t.Fatalf("acceptance did not create an assignment: %s",acceptResponse.Body.String())
    }

    startResponse,_ := call(http.MethodPost,"/v1/professional/orders/"+orderID+"/transition",tokens["professional"],"",map[string]string{"to":"in_progress"})
    if startResponse.Code != http.StatusOK {
        t.Fatalf("start order: status=%d body=%s",startResponse.Code,startResponse.Body.String())
    }
    completeResponse,_ := call(http.MethodPost,"/v1/professional/orders/"+orderID+"/transition",tokens["professional"],"",map[string]string{"to":"completed"})
    if completeResponse.Code != http.StatusOK {
        t.Fatalf("complete order: status=%d body=%s",completeResponse.Code,completeResponse.Body.String())
    }

    timelineResponse,timeline := call(http.MethodGet,"/v1/orders/"+orderID,tokens["customer"],"",nil)
    if timelineResponse.Code != http.StatusOK || timeline["order"].(map[string]any)["status"]!="completed" {
        t.Fatalf("customer timeline: status=%d body=%s",timelineResponse.Code,timelineResponse.Body.String())
    }
    crossTenantResponse,_ := call(http.MethodGet,"/v1/orders/"+orderID,tokens["other"],"",nil)
    if crossTenantResponse.Code != http.StatusNotFound {
        t.Fatalf("customer must not read another customer's order: status=%d body=%s",crossTenantResponse.Code,crossTenantResponse.Body.String())
    }

    operatorOrdersResponse,operatorOrders := call(http.MethodGet,"/v1/operator/orders",tokens["operator"],"",nil)
    if operatorOrdersResponse.Code != http.StatusOK || len(operatorOrders["orders"].([]any))!=1 {
        t.Fatalf("operator order queue: status=%d body=%s",operatorOrdersResponse.Code,operatorOrdersResponse.Body.String())
    }

    scheduleTwo := time.Now().UTC().Add(4*time.Hour).Truncate(time.Microsecond)
    orderInput["scheduled_at"] = scheduleTwo.Format(time.RFC3339Nano)
    secondOrderResponse,secondOrderBody := call(http.MethodPost,"/v1/orders",tokens["customer"],"mvp-order-2-"+suffix,orderInput)
    if secondOrderResponse.Code != http.StatusCreated {
        t.Fatalf("create second order: status=%d body=%s",secondOrderResponse.Code,secondOrderResponse.Body.String())
    }
    secondOrderID := secondOrderBody["order"].(map[string]any)["id"].(string)
    secondDispatchResponse,secondDispatchBody := call(http.MethodPost,"/v1/operator/orders/"+secondOrderID+"/dispatch",tokens["operator"],"",nil)
    if secondDispatchResponse.Code != http.StatusCreated {
        t.Fatalf("dispatch second order: status=%d body=%s",secondDispatchResponse.Code,secondDispatchResponse.Body.String())
    }
    secondOfferID := secondDispatchBody["offers"].([]any)[0].(map[string]any)["id"].(string)
    secondAcceptResponse,_ := call(http.MethodPost,"/v1/professional/offers/"+secondOfferID+"/accept",tokens["professional"],"",nil)
    if secondAcceptResponse.Code != http.StatusOK {
        t.Fatalf("accept second order: status=%d body=%s",secondAcceptResponse.Code,secondAcceptResponse.Body.String())
    }
    cancelResponse,cancelBody := call(http.MethodPost,"/v1/orders/"+secondOrderID+"/cancel",tokens["customer"],"mvp-cancel-1-"+suffix,map[string]string{"reason":"customer requested cancellation"})
    if cancelResponse.Code != http.StatusOK || cancelBody["funds_moved"]!=false {
        t.Fatalf("cancel order: status=%d body=%s",cancelResponse.Code,cancelResponse.Body.String())
    }
    var bookingStatus string
    if err = pool.QueryRow(ctx,`SELECT status FROM provider_bookings WHERE order_id=$1`,secondOrderID).Scan(&bookingStatus); err != nil {
        t.Fatal(err)
    }
    if bookingStatus!="cancelled" {
        t.Fatalf("cancellation must release the provider booking, got %q",bookingStatus)
    }
}
