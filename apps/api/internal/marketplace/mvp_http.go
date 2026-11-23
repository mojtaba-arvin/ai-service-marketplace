package marketplace

import (
    "context"
    "encoding/json"
    "errors"
    "net/http"
    "strconv"
    "strings"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

type PublicCatalogItem struct {
    CategoryID string `json:"category_id"`
    ParentID *string `json:"parent_id,omitempty"`
    CategorySlug string `json:"category_slug"`
    CategoryName string `json:"category_name"`
    MenuID string `json:"menu_id"`
    ServiceCode string `json:"service_code"`
    AmountMinor int64 `json:"amount_minor"`
    Currency string `json:"currency"`
}

type ProfessionalOrder struct {
    ID string `json:"id"`
    AmountMinor int64 `json:"amount_minor"`
    Currency string `json:"currency"`
    Status string `json:"status"`
    ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
    CountryCode *string `json:"country_code,omitempty"`
    RegionCode *string `json:"region_code,omitempty"`
    CityCode *string `json:"city_code,omitempty"`
    AreaCode *string `json:"area_code,omitempty"`
}

type OperatorProvider struct {
    ID string `json:"id"`
    IdentityID string `json:"identity_id"`
    DisplayName string `json:"display_name"`
    Active bool `json:"active"`
    VerifiedAt *time.Time `json:"verified_at,omitempty"`
    CategoryID *string `json:"category_id,omitempty"`
    SkillVerifiedAt *time.Time `json:"skill_verified_at,omitempty"`
}

type OperatorOrder struct {
    ID string `json:"id"`
    CustomerID string `json:"customer_id"`
    AmountMinor int64 `json:"amount_minor"`
    Currency string `json:"currency"`
    Status string `json:"status"`
    ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
    CountryCode *string `json:"country_code,omitempty"`
    RegionCode *string `json:"region_code,omitempty"`
    CityCode *string `json:"city_code,omitempty"`
    AreaCode *string `json:"area_code,omitempty"`
    AssignedProviderID *string `json:"assigned_provider_id,omitempty"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

func (s *Store) ListPublicCatalog(ctx context.Context, marketCode string) ([]PublicCatalogItem, error) {
    if _, err := s.GetMarket(ctx, marketCode); err != nil {
        return nil, err
    }
    rows, err := s.DB.Query(ctx, `
        SELECT c.id,c.parent_id,c.slug,COALESCE(t.display_name,c.display_name),
            m.id,m.service_code,v.amount_minor,v.currency
        FROM service_categories c
        JOIN market_category_visibility visibility
          ON visibility.category_id=c.id AND visibility.market_code=$1 AND visibility.visible=true
        LEFT JOIN category_translations t
          ON t.category_id=c.id AND t.market_code=$1
         AND t.locale=(SELECT default_locale FROM markets WHERE code=$1 AND enabled=true)
        JOIN service_price_menus m ON m.category_id=c.id
        JOIN service_price_versions v ON v.id=m.active_version_id
        JOIN markets market ON market.code=$1 AND market.enabled=true AND market.currency=v.currency
        WHERE c.enabled=true
        ORDER BY c.slug,m.service_code
    `, strings.ToUpper(strings.TrimSpace(marketCode)))
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    items := make([]PublicCatalogItem, 0)
    for rows.Next() {
        var item PublicCatalogItem
        if err = rows.Scan(&item.CategoryID,&item.ParentID,&item.CategorySlug,&item.CategoryName,
            &item.MenuID,&item.ServiceCode,&item.AmountMinor,&item.Currency); err != nil {
            return nil, err
        }
        items = append(items, item)
    }
    if err = rows.Err(); err != nil {
        return nil, err
    }
    return items, nil
}

func (s *Store) ListProfessionalOffers(ctx context.Context, actor Actor) ([]DispatchOffer, error) {
    if actor.Role != RoleProfessional || actor.SubjectID == "" || actor.TenantID == "" {
        return nil, ErrForbidden
    }
    rows, err := s.DB.Query(ctx, `
        SELECT off.id,off.round_id,off.order_id,off.provider_id,off.start_at,off.end_at,
            off.timezone,off.status,off.expires_at,off.created_at
        FROM dispatch_offers off
        JOIN provider_profiles p ON p.id=off.provider_id
        WHERE p.identity_id=$1 AND p.tenant_id=$2
          AND off.status='offered' AND off.expires_at>now()
        ORDER BY off.expires_at,off.created_at
        LIMIT 50
    `, actor.SubjectID, actor.TenantID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    offers := make([]DispatchOffer, 0)
    for rows.Next() {
        var offer DispatchOffer
        if err = rows.Scan(&offer.ID,&offer.RoundID,&offer.OrderID,&offer.ProviderID,&offer.StartAt,
            &offer.EndAt,&offer.Timezone,&offer.Status,&offer.ExpiresAt,&offer.CreatedAt); err != nil {
            return nil, err
        }
        offers = append(offers, offer)
    }
    if err = rows.Err(); err != nil {
        return nil, err
    }
    return offers, nil
}

func (s *Store) ListOperatorOrders(ctx context.Context, actor Actor, marketCode string) ([]OperatorOrder, error) {
    if actor.Role != RoleOperator || actor.SubjectID == "" || actor.TenantID == "" {
        return nil, ErrForbidden
    }
    if err := Authorize(actor, Resource{TenantID:actor.TenantID}, ActionOrderRead); err != nil {
        return nil, err
    }
    rows, err := s.DB.Query(ctx, `
        SELECT id,customer_id,amount_minor,currency,status,scheduled_at,country_code,
            region_code,city_code,area_code,assigned_provider_id,created_at,updated_at
        FROM orders
        WHERE tenant_id=$1 AND country_code=$2
        ORDER BY created_at DESC,id DESC
        LIMIT 100
    `, actor.TenantID, strings.ToUpper(strings.TrimSpace(marketCode)))
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    orders := make([]OperatorOrder, 0)
    for rows.Next() {
        var order OperatorOrder
        if err = rows.Scan(&order.ID,&order.CustomerID,&order.AmountMinor,&order.Currency,&order.Status,
            &order.ScheduledAt,&order.CountryCode,&order.RegionCode,&order.CityCode,&order.AreaCode,
            &order.AssignedProviderID,&order.CreatedAt,&order.UpdatedAt); err != nil {
            return nil, err
        }
        orders = append(orders, order)
    }
    if err = rows.Err(); err != nil {
        return nil, err
    }
    return orders, nil
}

func (s *Store) ListProfessionalOrders(ctx context.Context, actor Actor) ([]ProfessionalOrder, error) {
    if actor.Role != RoleProfessional || actor.SubjectID == "" || actor.TenantID == "" {
        return nil, ErrForbidden
    }
    rows, err := s.DB.Query(ctx, `
        SELECT o.id,o.amount_minor,o.currency,o.status,o.scheduled_at,
            o.country_code,o.region_code,o.city_code,o.area_code
        FROM orders o
        JOIN provider_profiles p ON p.id=o.assigned_provider_id
        WHERE p.identity_id=$1 AND p.tenant_id=$2
          AND o.tenant_id=p.tenant_id
          AND o.status IN ('accepted','in_progress')
        ORDER BY o.scheduled_at,o.created_at
        LIMIT 100
    `,actor.SubjectID,actor.TenantID)
    if err != nil { return nil,err }
    defer rows.Close()
    orders:=make([]ProfessionalOrder,0)
    for rows.Next() {
        var order ProfessionalOrder
        if err=rows.Scan(&order.ID,&order.AmountMinor,&order.Currency,&order.Status,&order.ScheduledAt,
            &order.CountryCode,&order.RegionCode,&order.CityCode,&order.AreaCode);err!=nil {
            return nil,err
        }
        orders=append(orders,order)
    }
    if err=rows.Err();err!=nil { return nil,err }
    return orders,nil
}

func (s *Store) ListOperatorProviders(ctx context.Context, actor Actor) ([]OperatorProvider, error) {
    if actor.Role != RoleOperator || actor.SubjectID == "" || actor.TenantID == "" {
        return nil, ErrForbidden
    }
    if err := Authorize(actor,Resource{TenantID:actor.TenantID},ActionProviderVerify); err != nil {
        return nil, err
    }
    rows, err := s.DB.Query(ctx, `
        SELECT p.id,p.identity_id,p.display_name,p.active,p.verified_at,
            ps.category_id,ps.verified_at
        FROM provider_profiles p
        LEFT JOIN provider_skills ps ON ps.provider_id=p.id
        WHERE p.tenant_id=$1
        ORDER BY p.created_at DESC,p.id,ps.category_id
        LIMIT 200
    `,actor.TenantID)
    if err != nil { return nil,err }
    defer rows.Close()
    providers := make([]OperatorProvider,0)
    for rows.Next() {
        var provider OperatorProvider
        if err=rows.Scan(&provider.ID,&provider.IdentityID,&provider.DisplayName,&provider.Active,
            &provider.VerifiedAt,&provider.CategoryID,&provider.SkillVerifiedAt);err!=nil {
            return nil,err
        }
        providers=append(providers,provider)
    }
    if err=rows.Err();err!=nil { return nil,err }
    return providers,nil
}

func (s *Store) TransitionOrderAsActor(ctx context.Context, actor Actor, orderID, target string) (Order, error) {
    tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
    if err != nil {
        return Order{}, err
    }
    defer tx.Rollback(ctx)

    var current Order
    var assignedProviderID *string
    var orderTenantID *string
    err = tx.QueryRow(ctx, `
        SELECT id,customer_id,amount_minor,currency,status,price_version_id,scheduled_at,
            country_code,region_code,city_code,area_code,assigned_provider_id,tenant_id
        FROM orders WHERE id=$1 FOR UPDATE
    `, orderID).Scan(&current.ID,&current.CustomerID,&current.AmountMinor,&current.Currency,
        &current.Status,&current.PriceVersionID,&current.ScheduledAt,&current.CountryCode,
        &current.RegionCode,&current.CityCode,&current.AreaCode,&assignedProviderID,&orderTenantID)
    if errors.Is(err, pgx.ErrNoRows) {
        return Order{}, ErrOrderNotFound
    }
    if err != nil {
        return Order{}, err
    }

    switch actor.Role {
    case RoleCustomer:
        if orderTenantID == nil || Authorize(actor, Resource{TenantID:*orderTenantID,OwnerID:current.CustomerID}, ActionOrderCancel) != nil {
            return Order{}, ErrForbidden
        }
        // Cancellation has a separate policy/idempotency workflow. Never allow
        // the generic state-transition endpoint to bypass its fee/refund rules.
        if target == "cancelled" {
            return Order{}, ErrInvalidTransition
        }
        return Order{}, ErrForbidden
    case RoleProfessional:
        if assignedProviderID == nil {
            return Order{}, ErrForbidden
        }
        var identityID, tenantID string
        err = tx.QueryRow(ctx, `SELECT identity_id,tenant_id FROM provider_profiles WHERE id=$1`,
            *assignedProviderID).Scan(&identityID,&tenantID)
        if err != nil || orderTenantID == nil || tenantID != *orderTenantID || tenantID != actor.TenantID {
            return Order{}, ErrForbidden
        }
        action := ActionOrderStart
        if target == "completed" {
            action = ActionOrderComplete
        } else if target != "in_progress" {
            return Order{}, ErrForbidden
        }
        if Authorize(actor, Resource{TenantID:tenantID,AssignedProfessionalID:identityID}, action) != nil {
            return Order{}, ErrForbidden
        }
    default:
        return Order{}, ErrForbidden
    }

    if current.Status == target {
        if err = tx.Commit(ctx); err != nil {
            return Order{}, err
        }
        return current, nil
    }
    if !CanTransition(current.Status, target) {
        return Order{}, ErrInvalidTransition
    }
    _, err = tx.Exec(ctx, `UPDATE orders SET status=$2,updated_at=now() WHERE id=$1`, orderID, target)
    if err != nil {
        return Order{}, err
    }
    if orderTenantID == nil || strings.TrimSpace(*orderTenantID) == "" {
        return Order{}, ErrForbidden
    }
    payload, marshalErr := json.Marshal(map[string]string{"order_id":orderID,"from":current.Status,"to":target})
    if marshalErr != nil {
        return Order{}, marshalErr
    }
    eventID, err := newID()
    if err != nil {
        return Order{}, err
    }
    if _, err = outbox.AppendDomainEvent(ctx, tx, outbox.DomainEvent{
        ID:eventID, Type:"order.state_changed", Version:1, OccurredAt:time.Now().UTC(),
        TenantID:*orderTenantID, Aggregate:outbox.EventAggregate{Type:"order",ID:orderID}, Data:payload,
    }); err != nil {
        return Order{}, err
    }
    current.Status = target
    if err = tx.Commit(ctx); err != nil {
        return Order{}, err
    }
    return current, nil
}

func (h *Handler) actorForRole(r *http.Request, role Role) (Actor, error) {
    principal, err := h.principal(r)
    if err != nil {
        return Actor{}, err
    }
    if h.TestMode {
        return Actor{SubjectID:principal.IdentityID,TenantID:h.DefaultTenantID,Role:role}, nil
    }
    return h.Store.ResolveActor(r.Context(), principal.IdentityID, h.DefaultTenantID, role)
}

func (h *Handler) serveMVP(w http.ResponseWriter, r *http.Request) bool {
    if h.TestMode {
        return false
    }
    path := r.URL.Path
    switch {
    case r.Method == http.MethodGet && path == "/v1/organization-invitations":
        h.organizationInvitationList(w,r); return true
    case r.Method == http.MethodPost && path == "/v1/organizations":
        h.organizationCreate(w,r); return true
    case r.Method == http.MethodGet && path == "/v1/organizations":
        h.organizationList(w,r); return true
    case strings.HasPrefix(path,"/v1/organizations/") && strings.Contains(path,"/properties/"):
        parts:=strings.Split(strings.Trim(path,"/"),"/")
        if len(parts)<5 || parts[0]!="v1" || parts[1]!="organizations" || parts[2]=="" || len(parts[2])>200 {
            writeError(w,http.StatusNotFound,"not found"); return true
        }
        if h.organizationPropertyManagementRoutes(w,r,parts[2]) { return true }
    case strings.HasPrefix(path,"/v1/organizations/") && isOrganizationRootPath(path) && (r.Method==http.MethodPatch || r.Method==http.MethodDelete):
        parts:=strings.Split(strings.Trim(path,"/"),"/")
        if len(parts)!=3 || parts[0]!="v1" || parts[1]!="organizations" || parts[2]=="" || len(parts[2])>200 {
            writeError(w,http.StatusNotFound,"not found"); return true
        }
        if h.organizationManagementRoutes(w,r,parts[2]) { return true }
    case strings.HasPrefix(path,"/v1/organizations/") && (strings.Contains(path,"/memberships/") || strings.HasSuffix(path,"/invitations/accept")):
        parts:=strings.Split(strings.Trim(path,"/"),"/")
        if len(parts)<5 || parts[0]!="v1" || parts[1]!="organizations" || parts[2]=="" {
            writeError(w,http.StatusNotFound,"not found"); return true
        }
        if h.organizationMembershipRoutes(w,r,parts[2]) { return true }
    case r.Method == http.MethodPost && strings.HasPrefix(path,"/v1/organizations/") && strings.HasSuffix(path,"/properties"):
        organizationID,ok:=parseOrganizationPath(path)
        if !ok { writeError(w,http.StatusNotFound,"not found"); return true }
        h.organizationPropertyCreate(w,r,organizationID); return true
    case r.Method == http.MethodGet && strings.HasPrefix(path,"/v1/organizations/"):
        organizationID,ok:=parseOrganizationPath(path)
        if !ok { writeError(w,http.StatusNotFound,"not found"); return true }
        if strings.HasSuffix(path,"/properties") { h.organizationProperties(w,r,organizationID) } else { h.organizationGet(w,r,organizationID) }
        return true
    case r.Method == http.MethodPost && path == "/v1/operator/service-forms":
        h.createServiceFormDraft(w,r); return true
    case r.Method == http.MethodPost && strings.HasPrefix(path,"/v1/operator/service-forms/") && strings.HasSuffix(path,"/publish"):
        h.publishServiceFormVersion(w,r); return true
    case r.Method == http.MethodGet && path == "/v1/catalog/service-forms":
        h.getPublishedServiceForm(w,r); return true
    case r.Method == http.MethodGet && path == "/v1/catalog":
        h.publicCatalog(w,r); return true
    case r.Method == http.MethodPost && path == "/v1/orders":
        h.createScheduledOrder(w,r); return true
    case r.Method == http.MethodGet && path == "/v1/orders":
        h.orderHistory(w,r); return true
    case r.Method == http.MethodGet && strings.HasPrefix(path,"/v1/orders/"):
        h.orderTimeline(w,r); return true
    case r.Method == http.MethodPost && strings.HasPrefix(path,"/v1/orders/") && strings.HasSuffix(path,"/cancel"):
        h.cancelOrder(w,r); return true
    case r.Method == http.MethodGet && path == "/v1/professional/profile":
        h.professionalProfile(w,r); return true
    case r.Method == http.MethodPost && path == "/v1/professional/profile":
        h.registerProfessional(w,r); return true
    case r.Method == http.MethodGet && path == "/v1/professional/orders":
        h.professionalOrders(w,r); return true
    case r.Method == http.MethodPost && path == "/v1/professional/profile/qualification":
        h.professionalQualification(w,r); return true
    case r.Method == http.MethodGet && path == "/v1/professional/offers":
        h.professionalOffers(w,r); return true
    case r.Method == http.MethodPost && strings.HasPrefix(path,"/v1/professional/offers/"):
        h.respondToOffer(w,r); return true
    case r.Method == http.MethodPost && strings.HasPrefix(path,"/v1/professional/orders/") && strings.HasSuffix(path,"/transition"):
        h.professionalTransition(w,r); return true
    case r.Method == http.MethodGet && path == "/v1/operator/orders":
        h.operatorOrders(w,r); return true
    case r.Method == http.MethodGet && path == "/v1/operator/providers":
        h.operatorProviders(w,r); return true
    case r.Method == http.MethodPost && strings.HasPrefix(path,"/v1/operator/orders/") && strings.HasSuffix(path,"/dispatch"):
        h.dispatchOrder(w,r); return true
    case r.Method == http.MethodPost && strings.HasPrefix(path,"/v1/operator/providers/") && strings.HasSuffix(path,"/verify"):
        h.verifyProvider(w,r); return true
    }
    return false
}


func (h *Handler) platformOperatorForRequest(r *http.Request)(Actor,error){
 principal,err:=h.principal(r);if err!=nil{return Actor{},err}
 if h.TestMode{return Actor{SubjectID:principal.IdentityID,TenantID:platformTenantID,Role:RoleOperator},nil}
 return h.Store.ResolveActor(r.Context(),principal.IdentityID,platformTenantID,RoleOperator)
}
func (h *Handler) createServiceFormDraft(w http.ResponseWriter,r *http.Request){
 actor,err:=h.platformOperatorForRequest(r);if err!=nil{writeError(w,http.StatusForbidden,"platform operator membership is required");return}
 var in struct{MenuID string `json:"menu_id"`;MarketCode string `json:"market_code"`;Locale string `json:"locale"`;Schema json.RawMessage `json:"schema"`}
 if json.NewDecoder(r.Body).Decode(&in)!=nil||len(in.Schema)==0{writeError(w,http.StatusBadRequest,"invalid service form payload");return}
 form,err:=h.Store.CreateServiceFormDraft(r.Context(),actor,in.MenuID,in.MarketCode,in.Locale,in.Schema)
 if err!=nil{status:=http.StatusUnprocessableEntity;if errors.Is(err,ErrForbidden){status=http.StatusForbidden};if errors.Is(err,ErrPriceMenuNotFound){status=http.StatusNotFound};if errors.Is(err,ErrMarketUnavailable){status=http.StatusUnprocessableEntity};writeError(w,status,err.Error());return}
 writeJSON(w,http.StatusCreated,map[string]any{"form":form})
}
func (h *Handler) publishServiceFormVersion(w http.ResponseWriter,r *http.Request){
 actor,err:=h.actorForRole(r,RoleOperator);if err!=nil{writeError(w,http.StatusForbidden,"platform operator membership is required");return}
 parts:=strings.Split(strings.Trim(r.URL.Path,"/"),"/")
 if len(parts)!=5||parts[0]!="v1"||parts[1]!="operator"||parts[2]!="service-forms"||parts[3]==""||parts[4]!="publish"{writeError(w,http.StatusNotFound,"not found");return}
 form,err:=h.Store.PublishServiceFormVersion(r.Context(),actor,parts[3])
 if err!=nil{status:=http.StatusUnprocessableEntity;if errors.Is(err,ErrForbidden){status=http.StatusForbidden};if errors.Is(err,ErrServiceFormNotFound){status=http.StatusNotFound};writeError(w,status,err.Error());return}
 writeJSON(w,http.StatusOK,map[string]any{"form":form})
}
func (h *Handler) getPublishedServiceForm(w http.ResponseWriter,r *http.Request){
 menuID:=strings.TrimSpace(r.URL.Query().Get("menu_id"));marketCode:=strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("market")));if marketCode==""{marketCode=h.DefaultMarketCode}
 if menuID==""{writeError(w,http.StatusBadRequest,"menu_id is required");return}
 market,err:=h.Store.GetMarket(r.Context(),marketCode);if err!=nil{writeError(w,http.StatusNotFound,"market not found");return}
 locale:=strings.TrimSpace(r.URL.Query().Get("locale"));if locale==""{locale=market.DefaultLocale}
 form,err:=h.Store.GetPublishedServiceForm(r.Context(),menuID,marketCode,locale)
 if err!=nil{if errors.Is(err,ErrServiceFormNotFound){writeError(w,http.StatusNotFound,"published service form not found")}else{writeError(w,http.StatusInternalServerError,"could not load service form")};return}
 writeJSON(w,http.StatusOK,map[string]any{"form":form})
}

func (h *Handler) publicCatalog(w http.ResponseWriter, r *http.Request) {
    market, err := h.Store.GetMarket(r.Context(),h.DefaultMarketCode)
    if err != nil {
        writeError(w,http.StatusServiceUnavailable,"market is unavailable")
        return
    }
    items, err := h.Store.ListPublicCatalog(r.Context(),h.DefaultMarketCode)
    if err != nil {
        writeError(w,http.StatusServiceUnavailable,"catalog is unavailable")
        return
    }
    writeJSON(w,http.StatusOK,map[string]any{
        "market":market.Code,"currency":market.Currency,"timezone":market.Timezone,
        "locale":market.DefaultLocale,"items":items,
    })
}

func (h *Handler) createScheduledOrder(w http.ResponseWriter, r *http.Request) {
    actor, err := h.actorForRole(r,RoleCustomer)
    if err != nil {
        writeError(w,http.StatusForbidden,"customer membership is required")
        return
    }
    var in struct {
        MenuID string `json:"menu_id"`
        ScheduledAt time.Time `json:"scheduled_at"`
        CountryCode string `json:"country_code"`
        RegionCode string `json:"region_code"`
        CityCode string `json:"city_code"`
        AreaCode string `json:"area_code"`
    }
    if json.NewDecoder(r.Body).Decode(&in) != nil {
        writeError(w,http.StatusBadRequest,"invalid JSON")
        return
    }
    if strings.ToUpper(strings.TrimSpace(in.CountryCode)) != h.DefaultMarketCode {
        writeError(w,http.StatusUnprocessableEntity,"orders are limited to the configured launch market")
        return
    }
    order, created, err := h.Store.CreateScheduledOrder(r.Context(),actor,r.Header.Get("Idempotency-Key"),
        in.MenuID,in.ScheduledAt,in.CountryCode,in.RegionCode,in.CityCode,in.AreaCode)
    if err != nil {
        status := http.StatusUnprocessableEntity
        if errors.Is(err,ErrForbidden) { status=http.StatusForbidden }
        if errors.Is(err,ErrOrderIdempotencyConflict) { status=http.StatusConflict }
        if errors.Is(err,ErrPriceMenuNotFound) { status=http.StatusNotFound }
        writeError(w,status,err.Error())
        return
    }
    code := http.StatusOK
    if created { code=http.StatusCreated }
    writeJSON(w,code,map[string]any{"order":order,"created":created})
}

func (h *Handler) orderHistory(w http.ResponseWriter, r *http.Request) {
    actor, err := h.actorForRole(r,RoleCustomer)
    if err != nil {
        writeError(w,http.StatusForbidden,"customer membership is required")
        return
    }
    limit := 20
    if raw := r.URL.Query().Get("limit"); raw != "" {
        parsed, parseErr := strconv.Atoi(raw)
        if parseErr != nil {
            writeError(w,http.StatusBadRequest,"invalid limit")
            return
        }
        limit = parsed
    }
    page, err := h.Store.ListOrderHistory(r.Context(),actor,limit,nil)
    if err != nil {
        writeError(w,http.StatusInternalServerError,"could not load order history")
        return
    }
    writeJSON(w,http.StatusOK,page)
}

func (h *Handler) orderTimeline(w http.ResponseWriter, r *http.Request) {
    actor, err := h.actorForRole(r,RoleCustomer)
    if err != nil {
        writeError(w,http.StatusForbidden,"customer membership is required")
        return
    }
    id := strings.TrimPrefix(r.URL.Path,"/v1/orders/")
    if id == "" || strings.Contains(id,"/") {
        writeError(w,http.StatusNotFound,"not found")
        return
    }
    item, err := h.Store.GetOrderTimeline(r.Context(),actor,id)
    if err != nil {
        writeError(w,http.StatusNotFound,"order not found")
        return
    }
    writeJSON(w,http.StatusOK,item)
}

func (h *Handler) cancelOrder(w http.ResponseWriter, r *http.Request) {
    actor, err := h.actorForRole(r,RoleCustomer)
    if err != nil {
        writeError(w,http.StatusForbidden,"customer membership is required")
        return
    }
    parts := strings.Split(strings.Trim(r.URL.Path,"/"),"/")
    if len(parts) != 4 {
        writeError(w,http.StatusNotFound,"not found")
        return
    }
    if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
        writeError(w,http.StatusBadRequest,"Idempotency-Key is required")
        return
    }
    var in struct { Reason string `json:"reason"` }
    if json.NewDecoder(r.Body).Decode(&in) != nil || strings.TrimSpace(in.Reason) == "" {
        writeError(w,http.StatusBadRequest,"reason is required")
        return
    }
    cancellation, err := h.Store.CancelOrder(r.Context(),actor,parts[2],r.Header.Get("Idempotency-Key"),in.Reason)
    if err != nil {
        status := http.StatusConflict
        if errors.Is(err,ErrForbidden) { status=http.StatusForbidden }
        if errors.Is(err,ErrOrderNotFound) { status=http.StatusNotFound }
        if errors.Is(err,ErrCancellationPolicyUnavailable) { status=http.StatusUnprocessableEntity }
        writeError(w,status,err.Error())
        return
    }
    writeJSON(w,http.StatusOK,map[string]any{"cancellation":cancellation,"funds_moved":false})
}

func (h *Handler) professionalProfile(w http.ResponseWriter, r *http.Request) {
    actor, err := h.actorForRole(r,RoleProfessional)
    if err != nil {
        writeError(w,http.StatusForbidden,"professional membership is required")
        return
    }
    profile, err := h.Store.GetProviderProfileForActor(r.Context(),actor)
    if err != nil {
        writeError(w,http.StatusNotFound,"professional profile not found")
        return
    }
    var qualified bool
    err = h.Store.DB.QueryRow(r.Context(),`
        SELECT
            EXISTS(SELECT 1 FROM provider_skills WHERE provider_id=$1) AND
            EXISTS(SELECT 1 FROM provider_service_areas WHERE provider_id=$1 AND active=true) AND
            EXISTS(SELECT 1 FROM provider_availability_windows
                   WHERE provider_id=$1 AND window_type='available' AND end_at>now())
    `,profile.ID).Scan(&qualified)
    if err != nil {
        writeError(w,http.StatusInternalServerError,"could not load professional qualification")
        return
    }
    writeJSON(w,http.StatusOK,map[string]any{"profile":profile,"qualification_complete":qualified})
}

func (h *Handler) professionalOrders(w http.ResponseWriter, r *http.Request) {
    actor, err := h.actorForRole(r,RoleProfessional)
    if err != nil {
        writeError(w,http.StatusForbidden,"professional membership is required")
        return
    }
    orders, err := h.Store.ListProfessionalOrders(r.Context(),actor)
    if err != nil {
        writeError(w,http.StatusInternalServerError,"could not load assigned orders")
        return
    }
    writeJSON(w,http.StatusOK,map[string]any{"orders":orders})
}

func (h *Handler) registerProfessional(w http.ResponseWriter, r *http.Request) {
    principal, err := h.principal(r)
    if err != nil {
        writeError(w,http.StatusUnauthorized,"unauthorized")
        return
    }
    var in struct { DisplayName string `json:"display_name"` }
    if json.NewDecoder(r.Body).Decode(&in) != nil || strings.TrimSpace(in.DisplayName) == "" {
        writeError(w,http.StatusBadRequest,"display_name is required")
        return
    }
    if err = h.Store.EnsureIdentityMembership(r.Context(),principal.IdentityID,h.DefaultTenantID,RoleProfessional); err != nil {
        writeError(w,http.StatusInternalServerError,"could not register professional role")
        return
    }
    actor, err := h.Store.ResolveActor(r.Context(),principal.IdentityID,h.DefaultTenantID,RoleProfessional)
    if err != nil {
        writeError(w,http.StatusForbidden,"professional membership is unavailable")
        return
    }
    profile, err := h.Store.CreateProviderProfile(r.Context(),actor,in.DisplayName)
    if err != nil {
        writeError(w,http.StatusConflict,"professional profile could not be created")
        return
    }
    writeJSON(w,http.StatusCreated,map[string]any{"profile":profile})
}

func (h *Handler) professionalOffers(w http.ResponseWriter, r *http.Request) {
    actor, err := h.actorForRole(r,RoleProfessional)
    if err != nil {
        writeError(w,http.StatusForbidden,"professional membership is required")
        return
    }
    offers, err := h.Store.ListProfessionalOffers(r.Context(),actor)
    if err != nil {
        writeError(w,http.StatusInternalServerError,"could not load offers")
        return
    }
    writeJSON(w,http.StatusOK,map[string]any{"offers":offers})
}

func (h *Handler) respondToOffer(w http.ResponseWriter, r *http.Request) {
    actor, err := h.actorForRole(r,RoleProfessional)
    if err != nil {
        writeError(w,http.StatusForbidden,"professional membership is required")
        return
    }
    parts := strings.Split(strings.Trim(r.URL.Path,"/"),"/")
    if len(parts) != 5 || (parts[4] != "accept" && parts[4] != "decline") {
        writeError(w,http.StatusNotFound,"not found")
        return
    }
    offerID := parts[3]
    if parts[4] == "accept" {
        assignment, acceptErr := h.Store.AcceptDispatchOffer(r.Context(),actor,offerID)
        if acceptErr != nil {
            status := http.StatusConflict
            if errors.Is(acceptErr,ErrForbidden) { status=http.StatusForbidden }
            writeError(w,status,acceptErr.Error())
            return
        }
        writeJSON(w,http.StatusOK,map[string]any{"assignment":assignment})
        return
    }
    if err = h.Store.DeclineDispatchOffer(r.Context(),actor,offerID); err != nil {
        status := http.StatusConflict
        if errors.Is(err,ErrForbidden) { status=http.StatusForbidden }
        writeError(w,status,err.Error())
        return
    }
    writeJSON(w,http.StatusOK,map[string]string{"status":"declined"})
}

func (h *Handler) professionalTransition(w http.ResponseWriter, r *http.Request) {
    actor, err := h.actorForRole(r,RoleProfessional)
    if err != nil {
        writeError(w,http.StatusForbidden,"professional membership is required")
        return
    }
    parts := strings.Split(strings.Trim(r.URL.Path,"/"),"/")
    if len(parts) != 5 {
        writeError(w,http.StatusNotFound,"not found")
        return
    }
    var in struct { To string `json:"to"` }
    if json.NewDecoder(r.Body).Decode(&in) != nil {
        writeError(w,http.StatusBadRequest,"invalid JSON")
        return
    }
    order, err := h.Store.TransitionOrderAsActor(r.Context(),actor,parts[3],in.To)
    if err != nil {
        status := http.StatusConflict
        if errors.Is(err,ErrForbidden) { status=http.StatusForbidden }
        if errors.Is(err,ErrOrderNotFound) { status=http.StatusNotFound }
        writeError(w,status,err.Error())
        return
    }
    writeJSON(w,http.StatusOK,map[string]any{"order":order})
}

func (h *Handler) operatorOrders(w http.ResponseWriter, r *http.Request) {
    actor, err := h.actorForRole(r,RoleOperator)
    if err != nil {
        writeError(w,http.StatusForbidden,"operator membership is required")
        return
    }
    orders, err := h.Store.ListOperatorOrders(r.Context(),actor,h.DefaultMarketCode)
    if err != nil {
        writeError(w,http.StatusInternalServerError,"could not load operational orders")
        return
    }
    writeJSON(w,http.StatusOK,map[string]any{"orders":orders})
}

func (h *Handler) operatorProviders(w http.ResponseWriter, r *http.Request) {
    actor, err := h.actorForRole(r,RoleOperator)
    if err != nil {
        writeError(w,http.StatusForbidden,"operator membership is required")
        return
    }
    providers, err := h.Store.ListOperatorProviders(r.Context(),actor)
    if err != nil {
        writeError(w,http.StatusInternalServerError,"could not load providers")
        return
    }
    writeJSON(w,http.StatusOK,map[string]any{"providers":providers})
}

func (h *Handler) dispatchOrder(w http.ResponseWriter, r *http.Request) {
    actor, err := h.actorForRole(r,RoleOperator)
    if err != nil {
        writeError(w,http.StatusForbidden,"operator membership is required")
        return
    }
    parts := strings.Split(strings.Trim(r.URL.Path,"/"),"/")
    if len(parts) != 5 {
        writeError(w,http.StatusNotFound,"not found")
        return
    }
    orderID := parts[3]
    candidates, err := h.Store.FindDispatchCandidatesForOrder(r.Context(),actor,orderID,time.Time{},time.Time{})
    if err != nil || len(candidates) == 0 {
        writeError(w,http.StatusConflict,"no eligible professional is currently available")
        return
    }
    limit := 3
    if len(candidates) < limit { limit=len(candidates) }
    providerIDs := make([]string,0,limit)
    for i:=0;i<limit;i++ { providerIDs=append(providerIDs,candidates[i].ProviderID) }
    offers, err := h.Store.CreateDispatchOffers(r.Context(),actor,orderID,providerIDs,limit,time.Now().UTC().Add(10*time.Minute))
    if err != nil {
        writeError(w,http.StatusConflict,err.Error())
        return
    }
    writeJSON(w,http.StatusCreated,map[string]any{"offers":offers})
}

func (h *Handler) professionalQualification(w http.ResponseWriter, r *http.Request) {
    actor, err := h.actorForRole(r,RoleProfessional)
    if err != nil {
        writeError(w,http.StatusForbidden,"professional membership is required")
        return
    }
    profile, err := h.Store.GetProviderProfileForActor(r.Context(),actor)
    if err != nil {
        writeError(w,http.StatusNotFound,"professional profile not found")
        return
    }
    var in struct {
        CategoryID string `json:"category_id"`
        CountryCode string `json:"country_code"`
        RegionCode string `json:"region_code"`
        CityCode string `json:"city_code"`
        AreaCode string `json:"area_code"`
        AvailableFrom time.Time `json:"available_from"`
        AvailableUntil time.Time `json:"available_until"`
        Timezone string `json:"timezone"`
    }
    if json.NewDecoder(r.Body).Decode(&in) != nil || in.CategoryID == "" || in.CountryCode == "" ||
        in.RegionCode == "" || in.CityCode == "" || in.AreaCode == "" || in.AvailableFrom.IsZero() ||
        in.AvailableUntil.IsZero() || in.Timezone == "" {
        writeError(w,http.StatusBadRequest,"category, service area, availability range, and timezone are required")
        return
    }
    if strings.ToUpper(strings.TrimSpace(in.CountryCode)) != h.DefaultMarketCode {
        writeError(w,http.StatusUnprocessableEntity,"service areas are limited to the configured launch market")
        return
    }
    if err = h.Store.AddProviderSkill(r.Context(),actor,profile.ID,in.CategoryID); err == nil {
        err = h.Store.AddProviderServiceArea(r.Context(),actor,profile.ID,in.CountryCode,in.RegionCode,in.CityCode,in.AreaCode)
    }
    var window ProviderAvailabilityWindow
    if err == nil {
        window, err = h.Store.SetProviderAvailability(r.Context(),actor,profile.ID,"available",
            in.AvailableFrom,in.AvailableUntil,in.Timezone)
    }
    if err != nil {
        writeError(w,http.StatusUnprocessableEntity,err.Error())
        return
    }
    writeJSON(w,http.StatusCreated,map[string]any{"profile":profile,"availability":window,"verification_required":true})
}

func (h *Handler) verifyProvider(w http.ResponseWriter, r *http.Request) {
    actor, err := h.actorForRole(r,RoleOperator)
    if err != nil {
        writeError(w,http.StatusForbidden,"operator membership is required")
        return
    }
    parts := strings.Split(strings.Trim(r.URL.Path,"/"),"/")
    if len(parts) != 5 {
        writeError(w,http.StatusNotFound,"not found")
        return
    }
    var in struct { CategoryID string `json:"category_id"` }
    if json.NewDecoder(r.Body).Decode(&in) != nil || in.CategoryID == "" {
        writeError(w,http.StatusBadRequest,"category_id is required")
        return
    }
    providerID := parts[3]
    if err = h.Store.VerifyProviderProfile(r.Context(),actor,providerID); err == nil {
        err = h.Store.VerifyProviderSkill(r.Context(),actor,providerID,in.CategoryID)
    }
    if err == nil {
        err = h.Store.SetProviderActive(r.Context(),actor,providerID,true)
    }
    if err != nil {
        writeError(w,http.StatusConflict,err.Error())
        return
    }
    writeJSON(w,http.StatusOK,map[string]string{"status":"verified"})
}


// organizationInvitationList returns invitations for the authenticated identity
// in the selected tenant. X-Tenant-ID selects context; the store query enforces
// both identity and tenant ownership and never trusts the header by itself.
func (h *Handler) organizationInvitationList(w http.ResponseWriter, r *http.Request) {
	principal, err := h.principal(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	items, err := h.Store.ListOrganizationInvitationsForIdentity(r.Context(), principal.IdentityID, h.organizationTenant(r))
	if err != nil {
		if errors.Is(err, ErrOrganizationAccessDenied) {
			writeError(w, http.StatusForbidden, "tenant context is required")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load organization invitations")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invitations": items})
}
