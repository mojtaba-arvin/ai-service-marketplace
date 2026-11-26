package marketplace

import (
	"bytes"
	"context"
	"errors"
	"encoding/json"
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
}

func (s *integrationEvidenceStorage) Put(_ context.Context, id string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
		"023_order_tenant_scope.sql", "031_operator_action_grants.sql",
		"032_operator_permission_event_integrity.sql", "033_incident_report_intake.sql",
		"034_incident_reviewer_workflow.sql", "035_private_incident_evidence.sql",
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
	if err = ensureTestOperatorGrants(ctx, pool, reviewer, ActionIncidentReview); err != nil {
		t.Fatal(err)
	}
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
	supportToken := issueToken(supportPhone)

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
	pdf := []byte("%PDF-1.7\nprivate incident evidence")

	upload := call(http.MethodPost, "/v1/incidents/"+report.ID+"/evidence", customerToken, pdf)
	if upload.Code != http.StatusCreated {
		t.Fatalf("reporter upload: status=%d body=%s", upload.Code, upload.Body.String())
	}
	var uploaded struct {
		EvidenceID string `json:"evidence_id"`
	}
	if err = json.Unmarshal(upload.Body.Bytes(), &uploaded); err != nil || len(uploaded.EvidenceID) != 32 {
		t.Fatalf("invalid upload response: %+v err=%v body=%s", uploaded, err, upload.Body.String())
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

	var successUploads, successDownloads, deniedUploads, deniedDownloads, deniedLists int
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
	if successUploads != 1 || successDownloads != 2 || deniedUploads != 3 || deniedDownloads != 1 || deniedLists != 2 {
		t.Fatalf("unexpected evidence audit totals: uploads=%d downloads=%d denied_uploads=%d denied_downloads=%d denied_lists=%d",
			successUploads, successDownloads, deniedUploads, deniedDownloads, deniedLists)
	}
}
