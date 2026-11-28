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
        "021_support_cases.sql","022_identity_memberships.sql","023_order_tenant_scope.sql","026_market_config_audit.sql","027_service_form_versions.sql","028_order_service_form_answers.sql","025_auth_devices.sql","029_order_completion_approval.sql","030_analytics_event_projection.sql","036_analytics_event_time_policy.sql",
    }
    for _, name := range migrations {
        raw, readErr := os.ReadFile(filepath.Join("..","..","migrations",name))
        if readErr != nil {
            t.Fatal(readErr)
        }
        if _, err = execMigrationScript(ctx, pool, raw); err != nil {
            t.Fatalf("apply %s: %v",name,err)
        }
    }

    suffix := fmt.Sprint(time.Now().UnixNano())
    tenantID := "mvp-test-" + suffix
    marketCode := ""
    startCode := time.Now().UnixNano() % 676
    for offset:=int64(0);offset<676;offset++ {
        value := (startCode+offset)%676
        candidate := string([]byte{byte('A'+value/26),byte('A'+value%26)})
        if candidate=="ZZ" { continue }
        var exists bool
        if err=pool.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM markets WHERE code=$1)`,candidate).Scan(&exists);err!=nil {
            t.Fatal(err)
        }
        if !exists { marketCode=candidate;break }
    }
    if marketCode=="" { t.Fatal("no unused two-letter market code available for test") }
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
    if err = ensureTestOperatorGrants(ctx,pool,operator,ActionProviderVerify,ActionOrderAssign,ActionOrderRead,ActionAnalyticsRead,ActionOperatorPermissionsManage,ActionIncidentReview); err != nil {
        t.Fatal(err)
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

    grantTargetID := newTestIdentity(t,ctx,pool)
    if err = store.EnsureIdentityMembership(ctx,grantTargetID,tenantID,RoleOperator); err != nil { t.Fatal(err) }
    permissionList, _ := call(http.MethodGet,"/v1/operator/permissions",tokens["operator"],"",nil)
    if permissionList.Code != http.StatusOK { t.Fatalf("operator permission list: status=%d body=%s",permissionList.Code,permissionList.Body.String()) }
    grantResponse, grantBody := call(http.MethodPost,"/v1/operator/permissions/grants",tokens["operator"],"",map[string]string{
        "identity_id":grantTargetID,"action":string(ActionOrderRead),"reason":"MVP HTTP permission grant test",
    })
    if grantResponse.Code != http.StatusCreated { t.Fatalf("operator permission grant: status=%d body=%s",grantResponse.Code,grantResponse.Body.String()) }
    grantObject, ok := grantBody["grant"].(map[string]any)
    if !ok || grantObject["id"] == nil { t.Fatalf("grant response missing grant ID: %#v",grantBody) }
    grantID := grantObject["id"].(string)
    revokeResponse, _ := call(http.MethodPost,"/v1/operator/permissions/grants/"+grantID+"/revoke",tokens["operator"],"",map[string]string{
        "reason":"MVP HTTP permission revocation test",
    })
    if revokeResponse.Code != http.StatusOK { t.Fatalf("operator permission revoke: status=%d body=%s",revokeResponse.Code,revokeResponse.Body.String()) }
    selfGrantResponse, _ := call(http.MethodPost,"/v1/operator/permissions/grants",tokens["operator"],"",map[string]string{
        "identity_id":operator.SubjectID,"action":string(ActionOrderRead),"reason":"self-grant must be denied",
    })
    if selfGrantResponse.Code != http.StatusForbidden { t.Fatalf("self-grant must be denied: status=%d body=%s",selfGrantResponse.Code,selfGrantResponse.Body.String()) }

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

    providersResponse,providersBody := call(http.MethodGet,"/v1/operator/providers",tokens["operator"],"",nil)
    if providersResponse.Code != http.StatusOK || len(providersBody["providers"].([]any))!=1 {
        t.Fatalf("operator provider queue: status=%d body=%s",providersResponse.Code,providersResponse.Body.String())
    }
    verifyResponse,_ := call(http.MethodPost,"/v1/operator/providers/"+profile.ID+"/verify",tokens["operator"],"",map[string]string{"category_id":category.ID})
    if verifyResponse.Code != http.StatusOK {
        t.Fatalf("verify provider: status=%d body=%s",verifyResponse.Code,verifyResponse.Body.String())
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
    incident, incidentErr := store.CreateIncidentReport(ctx, customer, IncidentReportInput{
        OrderID:orderID,Category:"safety_concern",Narrative:"The service visit left a safety hazard.",
    })
    if incidentErr != nil { t.Fatalf("create incident for reviewer HTTP test: %v",incidentErr) }
    reviewerQueue, _ := call(http.MethodGet,"/v1/operator/incidents",tokens["operator"],"",nil)
    if reviewerQueue.Code != http.StatusOK { t.Fatalf("reviewer incident queue: status=%d body=%s",reviewerQueue.Code,reviewerQueue.Body.String()) }
    reviewerDetail, _ := call(http.MethodGet,"/v1/operator/incidents/"+incident.ID,tokens["operator"],"",nil)
    if reviewerDetail.Code != http.StatusOK { t.Fatalf("reviewer incident detail: status=%d body=%s",reviewerDetail.Code,reviewerDetail.Body.String()) }
    transitionResponse, _ := call(http.MethodPost,"/v1/operator/incidents/"+incident.ID+"/transition",tokens["operator"],"",map[string]string{
        "action":"status","status":"triaged","reason":"HTTP reviewer triage test",
    })
    if transitionResponse.Code != http.StatusOK { t.Fatalf("reviewer incident transition: status=%d body=%s",transitionResponse.Code,transitionResponse.Body.String()) }
    reviewerTargetID := newTestIdentity(t,ctx,pool)
    if err = store.EnsureIdentityMembership(ctx,reviewerTargetID,tenantID,RoleOperator); err != nil { t.Fatal(err) }
    if _, err = store.GrantOperatorAction(ctx,operator,tenantID,reviewerTargetID,ActionIncidentReview,"HTTP reviewer assignment target"); err != nil { t.Fatal(err) }
    assignmentResponse, _ := call(http.MethodPost,"/v1/operator/incidents/"+incident.ID+"/transition",tokens["operator"],"",map[string]string{
        "action":"assign","assigned_reviewer_identity_id":reviewerTargetID,"reason":"HTTP reviewer assignment test",
    })
    if assignmentResponse.Code != http.StatusOK { t.Fatalf("reviewer incident assignment: status=%d body=%s",assignmentResponse.Code,assignmentResponse.Body.String()) }

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
    prematureApproval,_ := call(http.MethodPost,"/v1/orders/"+orderID+"/completion-approval",tokens["customer"],"",nil)
    if prematureApproval.Code != http.StatusConflict {
        t.Fatalf("customer approval before professional completion must conflict: status=%d body=%s",prematureApproval.Code,prematureApproval.Body.String())
    }
    completeResponse,_ := call(http.MethodPost,"/v1/professional/orders/"+orderID+"/transition",tokens["professional"],"",map[string]string{"to":"completed"})
    if completeResponse.Code != http.StatusOK {
        t.Fatalf("complete order: status=%d body=%s",completeResponse.Code,completeResponse.Body.String())
    }

    otherApproval,_ := call(http.MethodPost,"/v1/orders/"+orderID+"/completion-approval",tokens["other"],"",nil)
    if otherApproval.Code != http.StatusForbidden {
        t.Fatalf("another customer must not approve this order: status=%d body=%s",otherApproval.Code,otherApproval.Body.String())
    }
    approvalResponse,approvalBody := call(http.MethodPost,"/v1/orders/"+orderID+"/completion-approval",tokens["customer"],"",nil)
    if approvalResponse.Code != http.StatusCreated || approvalBody["created"] != true {
        t.Fatalf("customer completion approval: status=%d body=%s",approvalResponse.Code,approvalResponse.Body.String())
    }
    approval := approvalBody["approval"].(map[string]any)
    approvalReplay,approvalReplayBody := call(http.MethodPost,"/v1/orders/"+orderID+"/completion-approval",tokens["customer"],"",nil)
    if approvalReplay.Code != http.StatusOK || approvalReplayBody["created"] != false ||
        approvalReplayBody["approval"].(map[string]any)["approved_at"] != approval["approved_at"] {
        t.Fatalf("completion approval replay must return the original approval: status=%d body=%s",approvalReplay.Code,approvalReplay.Body.String())
    }
    var approvalCount int
    if err = pool.QueryRow(ctx,`SELECT count(*) FROM order_completion_approvals WHERE order_id=$1`,orderID).Scan(&approvalCount); err != nil || approvalCount != 1 {
        t.Fatalf("expected exactly one completion approval, count=%d err=%v",approvalCount,err)
    }
    if _,err = pool.Exec(ctx,`UPDATE order_completion_approvals SET approved_at=now() WHERE order_id=$1`,orderID); err == nil {
        t.Fatal("completion approval history must be immutable")
    }

    timelineResponse,timeline := call(http.MethodGet,"/v1/orders/"+orderID,tokens["customer"],"",nil)
    if timelineResponse.Code != http.StatusOK || timeline["order"].(map[string]any)["status"]!="completed" {
        t.Fatalf("customer timeline: status=%d body=%s",timelineResponse.Code,timelineResponse.Body.String())
    }
    if timeline["completion_approval"] == nil {
        t.Fatalf("customer timeline must expose the immutable completion approval: %s",timelineResponse.Body.String())
    }
    crossTenantResponse,_ := call(http.MethodGet,"/v1/orders/"+orderID,tokens["other"],"",nil)
    if crossTenantResponse.Code != http.StatusNotFound {
        t.Fatalf("customer must not read another customer's order: status=%d body=%s",crossTenantResponse.Code,crossTenantResponse.Body.String())
    }

    operatorOrdersResponse,operatorOrders := call(http.MethodGet,"/v1/operator/orders",tokens["operator"],"",nil)
    if operatorOrdersResponse.Code != http.StatusOK || len(operatorOrders["orders"].([]any))!=1 {
        t.Fatalf("operator order queue: status=%d body=%s",operatorOrdersResponse.Code,operatorOrdersResponse.Body.String())
    }

    analyticsPath:="/v1/operator/analytics/summary?from="+time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)+"&to="+time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
    analyticsResponse,analyticsBody:=call(http.MethodGet,analyticsPath,tokens["operator"],"",nil)
    if analyticsResponse.Code!=http.StatusOK || analyticsResponse.Header().Get("Cache-Control")!="private, no-store" {
        t.Fatalf("operator analytics summary: status=%d headers=%v body=%s",analyticsResponse.Code,analyticsResponse.Header(),analyticsResponse.Body.String())
    }
    if ready,ok:=analyticsBody["data_ready"].(bool);!ok||ready {
        t.Fatalf("analytics must not claim readiness without an active populated projection: %#v",analyticsBody)
    }
    forbiddenAnalytics,_:=call(http.MethodGet,analyticsPath,tokens["customer"],"",nil)
    if forbiddenAnalytics.Code!=http.StatusForbidden { t.Fatalf("customer must not read operator analytics: status=%d",forbiddenAnalytics.Code) }
    invalidAnalytics,_:=call(http.MethodGet,"/v1/operator/analytics/summary?from=invalid&to=invalid",tokens["operator"],"",nil)
    if invalidAnalytics.Code!=http.StatusBadRequest { t.Fatalf("invalid analytics window must be rejected: status=%d",invalidAnalytics.Code) }

    // An operator must not see another tenant's orders, even when both tenants
    // use the same market/country configuration.
    otherTenant := "mvp-other-" + suffix
    otherCustomerID := newTestIdentity(t,ctx,pool)
    otherOperatorID := newTestIdentity(t,ctx,pool)
    otherCustomerActor := Actor{SubjectID:otherCustomerID,TenantID:otherTenant,Role:RoleCustomer}
    otherOperatorActor := Actor{SubjectID:otherOperatorID,TenantID:otherTenant,Role:RoleOperator}
    if err = store.EnsureIdentityMembership(ctx,otherCustomerID,otherTenant,RoleCustomer); err != nil { t.Fatal(err) }
    if err = store.EnsureIdentityMembership(ctx,otherOperatorID,otherTenant,RoleOperator); err != nil { t.Fatal(err) }
    if err = ensureTestOperatorGrants(ctx,pool,otherOperatorActor,ActionOrderRead); err != nil { t.Fatal(err) }
    otherTenantOrder,createdOther,createOtherErr := store.CreateScheduledOrder(ctx,otherCustomerActor,
        "other-tenant-order-"+suffix,menu.ID,time.Now().UTC().Add(6*time.Hour),marketCode,
        "Test Region","Test City","Test Area")
    if createOtherErr != nil || !createdOther { t.Fatalf("create other-tenant order: %#v created=%v err=%v",otherTenantOrder,createdOther,createOtherErr) }
    originalTenantOrders, listErr := store.ListOperatorOrders(ctx,operator,marketCode)
    if listErr != nil || len(originalTenantOrders)!=1 || originalTenantOrders[0].ID!=orderID {
        t.Fatalf("operator saw cross-tenant order: orders=%#v err=%v",originalTenantOrders,listErr)
    }
    otherTenantOrders, listErr := store.ListOperatorOrders(ctx,otherOperatorActor,marketCode)
    if listErr != nil || len(otherTenantOrders)!=1 || otherTenantOrders[0].ID!=otherTenantOrder.ID {
        t.Fatalf("other tenant operator queue: orders=%#v err=%v",otherTenantOrders,listErr)
    }

    newPrice,err := store.CreatePriceVersion(ctx,menu.ID,5100,"USD")
    if err != nil { t.Fatalf("publish updated price before reorder: %v",err) }
    reorderAt := time.Now().UTC().Add(8*time.Hour).Truncate(time.Microsecond)
    reorderPayload := map[string]any{"scheduled_at":reorderAt.Format(time.RFC3339Nano)}
    reorderResponse,reorderBody := call(http.MethodPost,"/v1/orders/"+orderID+"/reorder",tokens["customer"],"mvp-reorder-1-"+suffix,reorderPayload)
    if reorderResponse.Code != http.StatusCreated {
        t.Fatalf("reorder completed order: status=%d body=%s",reorderResponse.Code,reorderResponse.Body.String())
    }
    reordered := reorderBody["order"].(map[string]any)
    if reordered["id"] == orderID || reordered["amount_minor"] != float64(newPrice.AmountMinor) {
        t.Fatalf("reorder must create a distinct order using the current price: %#v",reordered)
    }
    reorderReplay,reorderReplayBody := call(http.MethodPost,"/v1/orders/"+orderID+"/reorder",tokens["customer"],"mvp-reorder-1-"+suffix,reorderPayload)
    if reorderReplay.Code != http.StatusOK || reorderReplayBody["created"] != false ||
        reorderReplayBody["order"].(map[string]any)["id"] != reordered["id"] {
        t.Fatalf("reorder idempotency replay failed: status=%d body=%s",reorderReplay.Code,reorderReplay.Body.String())
    }
    unavailablePayload := map[string]any{"scheduled_at":time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339Nano)}
    unavailableResponse,_ := call(http.MethodPost,"/v1/orders/"+orderID+"/reorder",tokens["customer"],"mvp-reorder-unavailable-"+suffix,unavailablePayload)
    if unavailableResponse.Code != http.StatusConflict {
        t.Fatalf("reorder must reject a slot without eligible professional availability: status=%d body=%s",unavailableResponse.Code,unavailableResponse.Body.String())
    }
    otherReorder,_ := call(http.MethodPost,"/v1/orders/"+orderID+"/reorder",tokens["other"],"mvp-reorder-other-"+suffix,reorderPayload)
    if otherReorder.Code != http.StatusNotFound {
        t.Fatalf("another customer must not reorder a customer order: status=%d body=%s",otherReorder.Code,otherReorder.Body.String())
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
