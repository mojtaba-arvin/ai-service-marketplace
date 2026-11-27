package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type integrationEvidenceStorage struct {
	mu      sync.Mutex
	objects map[string][]byte
	putErr  error
	getErr  error
}

func (s *integrationEvidenceStorage) Put(_ context.Context, id string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.putErr != nil {
		return s.putErr
	}
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	if _, exists := s.objects[id]; exists {
		return errors.New("object already exists")
	}
	s.objects[id] = bytes.Clone(data)
	return nil
}

func (s *integrationEvidenceStorage) Get(_ context.Context, id string, maxBytes int64) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return nil, s.getErr
	}
	data, ok := s.objects[id]
	if !ok {
		return nil, os.ErrNotExist
	}
	if int64(len(data)) > maxBytes+4096 {
		return nil, errors.New("object exceeds configured read bound")
	}
	return bytes.Clone(data), nil
}

func (s *integrationEvidenceStorage) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.objects[id]; !ok {
		return os.ErrNotExist
	}
	delete(s.objects, id)
	return nil
}

type integrationEvidenceScanner struct {
	err error
}

func (s *integrationEvidenceScanner) Scan(context.Context, []byte) error {
	return s.err
}

func newEvidenceHTTPIdentity(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	id := newTestID(t)
	phone := fmt.Sprintf("+1996%011d", time.Now().UnixNano()%100000000000)
	if _, err := pool.Exec(ctx, `INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`, id, phone); err != nil {
		t.Fatal(err)
	}
	return id, phone
}

func TestIncidentEvidenceHTTPPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	schema := "incident_evidence_http_" + fmt.Sprint(time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	}()

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	migrations := []string{
		"001_walking_skeleton.sql", "002_identity.sql", "003_sessions.sql",
		"004_catalog.sql", "005_pricing.sql", "006_provider_eligibility.sql",
		"015_order_schedule.sql", "018_dispatch_offers.sql", "022_identity_memberships.sql",
		"023_order_tenant_scope.sql", "025_auth_devices.sql", "031_operator_action_grants.sql",
		"032_operator_permission_event_integrity.sql", "033_incident_report_intake.sql",
		"034_incident_reviewer_workflow.sql", "035_private_incident_evidence.sql", "036_incident_evidence_lifecycle.sql",
	}
	for _, name := range migrations {
		raw, readErr := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err = execMigrationScript(ctx, pool, raw); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}

	customerID, customerPhone := newEvidenceHTTPIdentity(t, ctx, pool)
	outsiderID, outsiderPhone := newEvidenceHTTPIdentity(t, ctx, pool)
	professionalID, _ := newEvidenceHTTPIdentity(t, ctx, pool)
	reviewerID, reviewerPhone := newEvidenceHTTPIdentity(t, ctx, pool)
	managerID, managerPhone := newEvidenceHTTPIdentity(t, ctx, pool)
	supportID, supportPhone := newEvidenceHTTPIdentity(t, ctx, pool)
	tenantID := "evidence-http-tenant-" + newTestID(t)
	for _, membership := range []struct {
		identity string
		role     Role
	}{
		{customerID, RoleCustomer},
		{outsiderID, RoleCustomer},
		{professionalID, RoleProfessional},
		{reviewerID, RoleOperator},
		{managerID, RoleOperator},
		{supportID, RoleSupport},
	} {
		if _, err = pool.Exec(ctx, `
			INSERT INTO identity_memberships(id,identity_id,tenant_id,role)
			VALUES($1,$2,$3,$4)
		`, "membership-"+membership.identity, membership.identity, tenantID, string(membership.role)); err != nil {
			t.Fatal(err)
		}
	}

	providerID := "provider-" + newTestID(t)
	if _, err = pool.Exec(ctx, `
		INSERT INTO provider_profiles(id,identity_id,tenant_id,display_name)
		VALUES($1,$2,$3,'Evidence HTTP professional')
	`, providerID, professionalID, tenantID); err != nil {
		t.Fatal(err)
	}
	orderID := "order-" + newTestID(t)
	if _, err = pool.Exec(ctx, `
		INSERT INTO orders(id,customer_id,idempotency_key,amount_minor,currency,status,tenant_id,assigned_provider_id)
		VALUES($1,$2,$3,1000,'OMR','requested',$4,$5)
	`, orderID, customerID, "evidence-http-"+newTestID(t), tenantID, providerID); err != nil {
		t.Fatal(err)
	}

	store := &Store{DB: pool}
	customer := Actor{SubjectID: customerID, TenantID: tenantID, Role: RoleCustomer}
	reviewer := Actor{SubjectID: reviewerID, TenantID: tenantID, Role: RoleOperator}
	manager := Actor{SubjectID: managerID, TenantID: tenantID, Role: RoleOperator}
	if err = ensureTestOperatorGrants(ctx, pool, reviewer, ActionIncidentReview); err != nil {
		t.Fatal(err)
	}
	grantAction := func(actor Actor, action Action) {
		t.Helper()
		grantID, grantErr := newID()
		if grantErr != nil {
			t.Fatal(grantErr)
		}
		if _, grantErr = pool.Exec(ctx, `
			INSERT INTO operator_action_grants
				(id,identity_id,tenant_id,action,granted_by_kind,grant_reason)
			VALUES($1,$2,$3,$4,'system_bootstrap','Explicit integration-test fixture grant')
		`, grantID, actor.SubjectID, actor.TenantID, string(action)); grantErr != nil {
			t.Fatal(grantErr)
		}
		if _, grantErr = pool.Exec(ctx, `
			INSERT INTO operator_permission_events
				(grant_id,tenant_id,identity_id,action,event_type,actor_kind,reason,after_state)
			VALUES($1,$2,$3,$4,'granted','system_bootstrap',
				'Explicit integration-test fixture grant','{"active":true}'::jsonb)
		`, grantID, actor.TenantID, actor.SubjectID, string(action)); grantErr != nil {
			t.Fatal(grantErr)
		}
	}
	grantAction(manager, ActionIncidentEvidenceManage)
	report, err := store.CreateIncidentReport(ctx, customer, IncidentReportInput{
		OrderID: orderID, Category: "safety_concern",
		Narrative: "A sharp object was left in the customer service area.",
	})
	if err != nil {
		t.Fatalf("create incident report: %v", err)
	}

	pepper := []byte("incident-evidence-http-session-pepper-32-bytes")
	issueToken := func(phone string) string {
		t.Helper()
		tokens, issueErr := store.IssueSession(ctx, VerifiedIdentity{phone: phone}, pepper, 15*time.Minute, time.Hour)
		if issueErr != nil {
			t.Fatal(issueErr)
		}
		return tokens.AccessToken
	}
	customerToken := issueToken(customerPhone)
	outsiderToken := issueToken(outsiderPhone)
	reviewerToken := issueToken(reviewerPhone)
	managerToken := issueToken(managerPhone)
	supportToken := issueToken(supportPhone)

	unprivilegedID, unprivilegedPhone := newEvidenceHTTPIdentity(t, ctx, pool)
	if _, err = pool.Exec(ctx, `
		INSERT INTO identity_memberships(id,identity_id,tenant_id,role)
		VALUES($1,$2,$3,'operator')
	`, "membership-"+unprivilegedID, unprivilegedID, tenantID); err != nil {
		t.Fatal(err)
	}
	otherTenant := "evidence-http-other-tenant-" + newTestID(t)
	otherReviewerID, otherReviewerPhone := newEvidenceHTTPIdentity(t, ctx, pool)
	otherManagerID, otherManagerPhone := newEvidenceHTTPIdentity(t, ctx, pool)
	otherReviewer := Actor{SubjectID: otherReviewerID, TenantID: otherTenant, Role: RoleOperator}
	otherManager := Actor{SubjectID: otherManagerID, TenantID: otherTenant, Role: RoleOperator}
	for _, identityID := range []string{otherReviewerID, otherManagerID} {
		if _, err = pool.Exec(ctx, `
			INSERT INTO identity_memberships(id,identity_id,tenant_id,role)
			VALUES($1,$2,$3,'operator')
		`, "membership-"+identityID, identityID, otherTenant); err != nil {
			t.Fatal(err)
		}
	}
	grantAction(otherReviewer, ActionIncidentReview)
	grantAction(otherManager, ActionIncidentEvidenceManage)
	otherCustomerID, otherCustomerPhone := newEvidenceHTTPIdentity(t, ctx, pool)
	if _, err = pool.Exec(ctx, `
		INSERT INTO identity_memberships(id,identity_id,tenant_id,role)
		VALUES($1,$2,$3,'customer')
	`, "membership-"+otherCustomerID, otherCustomerID, otherTenant); err != nil {
		t.Fatal(err)
	}
	otherOrderID := "order-" + newTestID(t)
	if _, err = pool.Exec(ctx, `
		INSERT INTO orders(id,customer_id,idempotency_key,amount_minor,currency,status,tenant_id)
		VALUES($1,$2,$3,1000,'OMR','requested',$4)
	`, otherOrderID, otherCustomerID, "evidence-http-"+newTestID(t), otherTenant); err != nil {
		t.Fatal(err)
	}
	otherReport, err := store.CreateIncidentReport(ctx, Actor{
		SubjectID: otherCustomerID, TenantID: otherTenant, Role: RoleCustomer,
	}, IncidentReportInput{
		OrderID: otherOrderID, Category: "safety_concern",
		Narrative: "A separate tenant incident used to verify isolation.",
	})
	if err != nil {
		t.Fatalf("create cross-tenant incident report: %v", err)
	}
	unprivilegedToken := issueToken(unprivilegedPhone)
	otherReviewerToken := issueToken(otherReviewerPhone)
	otherManagerToken := issueToken(otherManagerPhone)
	otherCustomerToken := issueToken(otherCustomerPhone)

	storage := &integrationEvidenceStorage{objects: make(map[string][]byte)}
	scanner := &integrationEvidenceScanner{}
	service := &EvidenceService{
		Storage: storage, Scanner: scanner,
		Keys: map[string][]byte{"integration-v1": []byte("0123456789abcdef0123456789abcdef")},
		ActiveKeyID: "integration-v1", MaxBytes: 1 << 20, MaxPerReport: 1,
		AllowedMediaType: map[string]bool{"application/pdf": true},
	}
	handler := &Handler{Store: store, Evidence: service, SessionPepper: pepper, DefaultTenantID: tenantID}

	call := func(method, path, token string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	callWithHeader := func(method, path, token string, body []byte, header, value string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set(header, value)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	otherTenantHandler := &Handler{Store: store, Evidence: service, SessionPepper: pepper, DefaultTenantID: otherTenant}
	callOtherTenant := func(method, path, token string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		otherTenantHandler.ServeHTTP(rec, req)
		return rec
	}
	pdf := []byte("%PDF-1.7\nprivate incident evidence")

	// Reject unsupported content, unbounded bodies, and unrelated reporters
	// before any object or metadata can be created.
	if invalid := call(http.MethodPost, "/v1/incidents/"+report.ID+"/evidence", customerToken, []byte("this is not a PDF")); invalid.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("malformed content must be rejected, got %d: %s", invalid.Code, invalid.Body.String())
	}
	oversizedBody := bytes.Repeat([]byte("A"), int(service.MaxBytes+1))
	if oversized := call(http.MethodPost, "/v1/incidents/"+report.ID+"/evidence", customerToken, oversizedBody); oversized.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized upload must be rejected, got %d: %s", oversized.Code, oversized.Body.String())
	}
	if denied := call(http.MethodPost, "/v1/incidents/"+report.ID+"/evidence", outsiderToken, pdf); denied.Code != http.StatusForbidden {
		t.Fatalf("unrelated reporter upload must be denied, got %d: %s", denied.Code, denied.Body.String())
	}
	storage.putErr = errors.New("private storage backend unavailable")
	if failed := call(http.MethodPost, "/v1/incidents/"+report.ID+"/evidence", customerToken, pdf); failed.Code != http.StatusServiceUnavailable {
		t.Fatalf("storage outage must fail closed, got %d: %s", failed.Code, failed.Body.String())
	}
	storage.putErr = nil

	upload := callWithHeader(http.MethodPost, "/v1/incidents/"+report.ID+"/evidence", customerToken, pdf,
		"Content-Disposition", "attachment; filename=\"../../../../etc/passwd\"")
	if upload.Code != http.StatusCreated {
		t.Fatalf("reporter upload: status=%d body=%s", upload.Code, upload.Body.String())
	}
	var uploaded struct {
		EvidenceID string `json:"evidence_id"`
	}
	if err = json.Unmarshal(upload.Body.Bytes(), &uploaded); err != nil || len(uploaded.EvidenceID) != 32 {
		t.Fatalf("invalid upload response: %+v err=%v body=%s", uploaded, err, upload.Body.String())
	}
	if bytes.Contains(upload.Body.Bytes(), []byte("url")) {
		t.Fatalf("upload response must not expose storage or download URLs: %s", upload.Body.String())
	}
	var defaultExpiry *time.Time
	if err = pool.QueryRow(ctx, "SELECT retention_expires_at FROM incident_evidence WHERE id=$1 AND tenant_id=$2", uploaded.EvidenceID, tenantID).Scan(&defaultExpiry); err != nil {
		t.Fatal(err)
	}
	if defaultExpiry != nil {
		t.Fatalf("evidence without an adopter retention policy must have no default expiry: %v", defaultExpiry)
	}

	list := call(http.MethodGet, "/v1/incidents/"+report.ID+"/evidence", customerToken, nil)
	if list.Code != http.StatusOK || !bytes.Contains(list.Body.Bytes(), []byte(uploaded.EvidenceID)) {
		t.Fatalf("reporter list: status=%d body=%s", list.Code, list.Body.String())
	}
	if list.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("evidence list cache policy = %q", list.Header().Get("Cache-Control"))
	}

	download := call(http.MethodGet, "/v1/incidents/"+report.ID+"/evidence/"+uploaded.EvidenceID, customerToken, nil)
	if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), pdf) {
		t.Fatalf("reporter download: status=%d body=%q", download.Code, download.Body.Bytes())
	}
	if download.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("download must set nosniff")
	}
	if got := download.Header().Get("Content-Disposition"); got != "attachment; filename=\"incident-evidence\"" {
		t.Fatalf("client filename must never be reflected in download headers, got %q", got)
	}
	if denied := call(http.MethodGet, "/v1/incidents/"+report.ID+"/evidence/"+uploaded.EvidenceID, outsiderToken, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("unrelated reporter download must be denied, got %d: %s", denied.Code, denied.Body.String())
	}
	if denied := call(http.MethodGet, "/v1/operator/incidents/"+report.ID+"/evidence/"+uploaded.EvidenceID, supportToken, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("support role download must be denied, got %d: %s", denied.Code, denied.Body.String())
	}
	if hidden := call(http.MethodGet, "/v1/operator/incidents/"+report.ID+"/evidence/"+uploaded.EvidenceID, otherReviewerToken, nil); hidden.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant reviewer download must not disclose evidence, got %d: %s", hidden.Code, hidden.Body.String())
	}

	reviewerList := call(http.MethodGet, "/v1/operator/incidents/"+report.ID+"/evidence", reviewerToken, nil)
	if reviewerList.Code != http.StatusOK {
		t.Fatalf("same-tenant reviewer list: status=%d body=%s", reviewerList.Code, reviewerList.Body.String())
	}
	reviewerDownload := call(http.MethodGet, "/v1/operator/incidents/"+report.ID+"/evidence/"+uploaded.EvidenceID, reviewerToken, nil)
	if reviewerDownload.Code != http.StatusOK || !bytes.Equal(reviewerDownload.Body.Bytes(), pdf) {
		t.Fatalf("same-tenant reviewer download: status=%d body=%q", reviewerDownload.Code, reviewerDownload.Body.Bytes())
	}

	if denied := call(http.MethodGet, "/v1/incidents/"+report.ID+"/evidence", outsiderToken, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("unrelated reporter must be denied, got %d: %s", denied.Code, denied.Body.String())
	}
	if denied := call(http.MethodGet, "/v1/operator/incidents/"+report.ID+"/evidence", supportToken, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("support role alone must be denied, got %d: %s", denied.Code, denied.Body.String())
	}
	if denied := call(http.MethodGet, "/v1/operator/incidents/"+report.ID+"/evidence", unprivilegedToken, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("operator without incident:review grant must be denied, got %d: %s", denied.Code, denied.Body.String())
	}
	if denied := call(http.MethodGet, "/v1/operator/incidents/"+report.ID+"/evidence", otherReviewerToken, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant reviewer must be denied, got %d: %s", denied.Code, denied.Body.String())
	}
	if denied := call(http.MethodGet, "/v1/incidents/"+otherReport.ID+"/evidence", customerToken, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant reporter must be denied, got %d: %s", denied.Code, denied.Body.String())
	}
	if visible := callOtherTenant(http.MethodGet, "/v1/incidents/"+otherReport.ID+"/evidence", otherCustomerToken, nil); visible.Code != http.StatusOK {
		t.Fatalf("other tenant's own reporter must retain access, got %d: %s", visible.Code, visible.Body.String())
	}

	// Scanner errors are fail-closed and separately classified in the audit log.
	scanner.err = errors.New("clamd connection unavailable")
	if failed := call(http.MethodPost, "/v1/incidents/"+report.ID+"/evidence", customerToken, pdf); failed.Code != http.StatusServiceUnavailable {
		t.Fatalf("scanner outage must fail closed, got %d: %s", failed.Code, failed.Body.String())
	}
	scanner.err = errors.New("malware scanner rejected content")
	if rejected := call(http.MethodPost, "/v1/incidents/"+report.ID+"/evidence", customerToken, pdf); rejected.Code != http.StatusServiceUnavailable {
		t.Fatalf("scanner rejection must fail closed, got %d: %s", rejected.Code, rejected.Body.String())
	}
	scanner.err = nil

	// Quota is checked transactionally after scanning; it cannot create a second row.
	if limited := call(http.MethodPost, "/v1/incidents/"+report.ID+"/evidence", customerToken, pdf); limited.Code != http.StatusConflict {
		t.Fatalf("per-report evidence quota must be enforced, got %d: %s", limited.Code, limited.Body.String())
	}

	if _, err = pool.Exec(ctx, `
		UPDATE incident_evidence SET retention_expires_at=now()-interval '1 second'
		WHERE id=$1 AND tenant_id=$2
	`, uploaded.EvidenceID, tenantID); err != nil {
		t.Fatal(err)
	}
	if expired := call(http.MethodGet, "/v1/incidents/"+report.ID+"/evidence/"+uploaded.EvidenceID, customerToken, nil); expired.Code != http.StatusGone {
		t.Fatalf("expired evidence must not be retrievable, got %d: %s", expired.Code, expired.Body.String())
	}


	// Lifecycle management is a separate, reasoned permission; reviewer and
	// support roles must not inherit it.
	lifecyclePath := "/v1/operator/incidents/" + report.ID + "/evidence/" + uploaded.EvidenceID + "/lifecycle"
	unauthorizedReviewerUpdate := call(http.MethodPatch, lifecyclePath, reviewerToken,
		[]byte(`{"legal_hold":true,"reason":"reviewer must not manage evidence"}`))
	if unauthorizedReviewerUpdate.Code != http.StatusForbidden {
		t.Fatalf("incident reviewer must not manage evidence lifecycle, got %d: %s", unauthorizedReviewerUpdate.Code, unauthorizedReviewerUpdate.Body.String())
	}
	if denied := call(http.MethodGet, "/v1/operator/incidents/"+report.ID+"/evidence/"+uploaded.EvidenceID, managerToken, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("evidence manager without incident:review must not download evidence, got %d: %s", denied.Code, denied.Body.String())
	}
	unauthorizedSupportUpdate := call(http.MethodPatch, lifecyclePath, supportToken,
		[]byte(`{"legal_hold":true,"reason":"support must not manage evidence"}`))
	if unauthorizedSupportUpdate.Code != http.StatusForbidden {
		t.Fatalf("support role must not manage evidence lifecycle, got %d: %s", unauthorizedSupportUpdate.Code, unauthorizedSupportUpdate.Body.String())
	}
	crossTenantManagerUpdate := callOtherTenant(http.MethodPatch, lifecyclePath, otherManagerToken,
		[]byte(`{"legal_hold":true,"reason":"cross-tenant attempt"}`))
	if crossTenantManagerUpdate.Code != http.StatusNotFound {
		t.Fatalf("other-tenant evidence manager must not see target evidence, got %d: %s", crossTenantManagerUpdate.Code, crossTenantManagerUpdate.Body.String())
	}
	managerUpdate := call(http.MethodPatch, lifecyclePath, managerToken,
		[]byte(`{"retention_expires_at":"2020-01-01T00:00:00Z","legal_hold":true,"reason":"Preserve evidence during active safety review"}`))
	if managerUpdate.Code != http.StatusOK {
		t.Fatalf("authorized evidence manager update: %d: %s", managerUpdate.Code, managerUpdate.Body.String())
	}
	if held := call(http.MethodGet, "/v1/incidents/"+report.ID+"/evidence/"+uploaded.EvidenceID, customerToken, nil); held.Code != http.StatusOK {
		t.Fatalf("legal hold must override an elapsed retrieval expiry, got %d: %s", held.Code, held.Body.String())
	}
	clearHold := call(http.MethodPatch, lifecyclePath, managerToken,
		[]byte(`{"legal_hold":false,"reason":"Safety review concluded; release legal hold"}`))
	if clearHold.Code != http.StatusOK {
		t.Fatalf("authorized legal-hold release: %d: %s", clearHold.Code, clearHold.Body.String())
	}
	if expiredAgain := call(http.MethodGet, "/v1/incidents/"+report.ID+"/evidence/"+uploaded.EvidenceID, customerToken, nil); expiredAgain.Code != http.StatusGone {
		t.Fatalf("clearing legal hold must restore expiry denial, got %d: %s", expiredAgain.Code, expiredAgain.Body.String())
	}
	clearExpiry := call(http.MethodPatch, lifecyclePath, managerToken,
		[]byte(`{"retention_expires_at":null,"reason":"Adopter policy explicitly clears the expiry"}`))
	if clearExpiry.Code != http.StatusOK {
		t.Fatalf("explicit expiry clearing: %d: %s", clearExpiry.Code, clearExpiry.Body.String())
	}
	if accessible := call(http.MethodGet, "/v1/incidents/"+report.ID+"/evidence/"+uploaded.EvidenceID, customerToken, nil); accessible.Code != http.StatusOK {
		t.Fatalf("clearing expiry must restore retrieval when no hold is set, got %d: %s", accessible.Code, accessible.Body.String())
	}
	var retentionUpdates, legalHoldSets, legalHoldClears int
	if err = pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE action='retention_update' AND outcome='success'),
			count(*) FILTER (WHERE action='legal_hold_set' AND outcome='success'),
			count(*) FILTER (WHERE action='legal_hold_cleared' AND outcome='success')
		FROM incident_evidence_access_events
		WHERE report_id=$1 AND tenant_id=$2
	`, report.ID, tenantID).Scan(&retentionUpdates, &legalHoldSets, &legalHoldClears); err != nil {
		t.Fatal(err)
	}
	if retentionUpdates != 2 || legalHoldSets != 1 || legalHoldClears != 1 {
		t.Fatalf("unexpected lifecycle audit totals: retention=%d hold_set=%d hold_clear=%d", retentionUpdates, legalHoldSets, legalHoldClears)
	}
	var lifecycleReason string
	if err = pool.QueryRow(ctx, `
		SELECT reason FROM incident_evidence_access_events
		WHERE report_id=$1 AND tenant_id=$2 AND action='legal_hold_set' AND outcome='success'
		ORDER BY id DESC LIMIT 1
	`, report.ID, tenantID).Scan(&lifecycleReason); err != nil || lifecycleReason != "Preserve evidence during active safety review" {
		t.Fatalf("lifecycle reason must be preserved in audit: %q, %v", lifecycleReason, err)
	}

	var successUploads, successDownloads, deniedUploads, deniedDownloads, deniedLists, crossTenantDenied int
	err = pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE action='upload' AND outcome='success'),
			count(*) FILTER (WHERE action='download' AND outcome='success'),
			count(*) FILTER (WHERE action='upload' AND outcome='denied'),
			count(*) FILTER (WHERE action='download' AND outcome='denied'),
			count(*) FILTER (WHERE action='list' AND outcome='denied')
		FROM incident_evidence_access_events
		WHERE report_id=$1 AND tenant_id=$2
	`, report.ID, tenantID).Scan(&successUploads, &successDownloads, &deniedUploads, &deniedDownloads, &deniedLists)
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `
		SELECT count(*) FROM incident_evidence_access_events
		WHERE report_id=$1 AND tenant_id=$2 AND action='list' AND outcome='denied'
	`, otherReport.ID, tenantID).Scan(&crossTenantDenied); err != nil {
		t.Fatal(err)
	}
	if successUploads != 1 || successDownloads != 4 || deniedUploads != 7 || deniedDownloads != 6 || deniedLists != 4 || crossTenantDenied != 1 {
		t.Fatalf("unexpected evidence audit totals: uploads=%d downloads=%d denied_uploads=%d denied_downloads=%d denied_lists=%d cross_tenant_denied_lists=%d",
			successUploads, successDownloads, deniedUploads, deniedDownloads, deniedLists, crossTenantDenied)
	}

	// Storage outages and missing objects are distinct fail-closed outcomes,
	// and both retrieval attempts must be audited without exposing bytes.
	storage.getErr = errors.New("private storage backend unavailable")
	if failed := call(http.MethodGet, "/v1/incidents/"+report.ID+"/evidence/"+uploaded.EvidenceID, customerToken, nil); failed.Code != http.StatusServiceUnavailable {
		t.Fatalf("storage read outage must return service unavailable, got %d: %s", failed.Code, failed.Body.String())
	}
	storage.getErr = nil
	storage.mu.Lock()
	delete(storage.objects, uploaded.EvidenceID)
	storage.mu.Unlock()
	if missing := call(http.MethodGet, "/v1/incidents/"+report.ID+"/evidence/"+uploaded.EvidenceID, customerToken, nil); missing.Code != http.StatusNotFound {
		t.Fatalf("missing private object must not be disclosed, got %d: %s", missing.Code, missing.Body.String())
	}
	var storageFailures, missingObjects int
	if err = pool.QueryRow(ctx, "SELECT count(*) FILTER (WHERE reason_category='storage_failure'), count(*) FILTER (WHERE reason_category='not_found') FROM incident_evidence_access_events WHERE report_id=$1 AND tenant_id=$2 AND action='download' AND outcome='denied'", report.ID, tenantID).Scan(&storageFailures, &missingObjects); err != nil {
		t.Fatal(err)
	}
	if storageFailures != 1 || missingObjects != 1 {
		t.Fatalf("missing audit classifications: storage failures=%d missing objects=%d", storageFailures, missingObjects)
	}

	var immutableAuditID int64
	if err = pool.QueryRow(ctx, "SELECT id FROM incident_evidence_access_events WHERE report_id=$1 AND tenant_id=$2 AND action='legal_hold_set' AND outcome='success' ORDER BY id DESC LIMIT 1", report.ID, tenantID).Scan(&immutableAuditID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE incident_evidence_access_events SET reason='rewritten' WHERE id=$1", immutableAuditID); err == nil {
		t.Fatal("evidence access audit rows must reject updates")
	}
	if _, err = pool.Exec(ctx, "DELETE FROM incident_evidence_access_events WHERE id=$1", immutableAuditID); err == nil {
		t.Fatal("evidence access audit rows must reject deletion")
	}
	var leakedAuditValues int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM incident_evidence_access_events WHERE report_id=$1 AND tenant_id=$2 AND (reason LIKE '%private incident evidence%' OR reason LIKE '%Bearer %' OR reason LIKE '%https://%')", report.ID, tenantID).Scan(&leakedAuditValues); err != nil {
		t.Fatal(err)
	}
	if leakedAuditValues != 0 {
		t.Fatalf("audit rows must not contain evidence bytes, credentials or URLs; found %d", leakedAuditValues)
	}
}
