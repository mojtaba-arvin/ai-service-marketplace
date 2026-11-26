package marketplace

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrEvidenceUnavailable = errors.New("incident evidence service unavailable")

type PrivateEvidenceStorage interface {
	Put(context.Context, string, []byte) error
	Get(context.Context, string, int64) ([]byte, error)
	Delete(context.Context, string) error
}

type FilesystemEvidenceStorage struct {
	Root string
}

type EvidenceScanner interface {
	Scan(context.Context, []byte) error
}

type ClamAVScanner struct {
	Address string
}

type EvidenceService struct {
	Storage          PrivateEvidenceStorage
	Scanner          EvidenceScanner
	Keys             map[string][]byte
	ActiveKeyID      string
	MaxBytes         int64
	MaxPerReport     int
	AllowedMediaType map[string]bool
	RetentionDays    int
}

type incidentEvidenceMetadata struct {
	ID             string     `json:"evidence_id"`
	MediaType      string     `json:"media_type"`
	SizeBytes      int64      `json:"size_bytes"`
	CreatedAt      time.Time  `json:"created_at"`
	RetentionUntil *time.Time `json:"retention_expires_at,omitempty"`
	LegalHold      bool       `json:"legal_hold"`
}

func NewEvidenceServiceFromEnv() (*EvidenceService, error) {
	root := strings.TrimSpace(os.Getenv("EVIDENCE_STORAGE_ROOT"))
	if !filepath.IsAbs(root) || root == string(filepath.Separator) {
		return nil, errors.New("EVIDENCE_STORAGE_ROOT must be an absolute private directory")
	}
	keyID := strings.TrimSpace(os.Getenv("EVIDENCE_ACTIVE_KEY_ID"))
	if keyID == "" || len(keyID) > 64 {
		return nil, errors.New("EVIDENCE_ACTIVE_KEY_ID is required and must be at most 64 characters")
	}
	var encoded map[string]string
	if err := json.Unmarshal([]byte(os.Getenv("EVIDENCE_ENCRYPTION_KEYS")), &encoded); err != nil || len(encoded) == 0 {
		return nil, errors.New("EVIDENCE_ENCRYPTION_KEYS must be a JSON object of key IDs to 32-byte hex keys")
	}
	keys := make(map[string][]byte, len(encoded))
	for id, value := range encoded {
		raw, err := hex.DecodeString(value)
		if err != nil || len(raw) != 32 || id == "" || len(id) > 64 {
			return nil, fmt.Errorf("evidence key %q must be a 32-byte hex value with a valid key ID", id)
		}
		keys[id] = raw
	}
	if _, ok := keys[keyID]; !ok {
		return nil, errors.New("EVIDENCE_ACTIVE_KEY_ID is not present in EVIDENCE_ENCRYPTION_KEYS")
	}
	maxBytes := int64(10 << 20)
	if raw := strings.TrimSpace(os.Getenv("EVIDENCE_MAX_BYTES")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 1 || value > 100<<20 {
			return nil, errors.New("EVIDENCE_MAX_BYTES must be between 1 and 104857600")
		}
		maxBytes = value
	}
	maxItems := 5
	if raw := strings.TrimSpace(os.Getenv("EVIDENCE_MAX_PER_REPORT")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 5 {
			return nil, errors.New("EVIDENCE_MAX_PER_REPORT must be between 1 and 5")
		}
		maxItems = value
	}
	retentionDays := 0
	if raw := strings.TrimSpace(os.Getenv("EVIDENCE_RETENTION_DAYS")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 36500 {
			return nil, errors.New("EVIDENCE_RETENTION_DAYS must be between 1 and 36500")
		}
		retentionDays = value
	}
	allowed := make(map[string]bool)
	for _, item := range strings.Split(os.Getenv("EVIDENCE_ALLOWED_MEDIA_TYPES"), ",") {
		value := strings.ToLower(strings.TrimSpace(item))
		if value != "" {
			allowed[value] = true
		}
	}
	if len(allowed) == 0 {
		return nil, errors.New("EVIDENCE_ALLOWED_MEDIA_TYPES must explicitly list allowed media types")
	}
	scanner := strings.TrimSpace(os.Getenv("EVIDENCE_CLAMD_ADDRESS"))
	if _, _, err := net.SplitHostPort(scanner); err != nil {
		return nil, errors.New("EVIDENCE_CLAMD_ADDRESS must be a private clamd host:port")
	}
	storage := &FilesystemEvidenceStorage{Root: root}
	if err := storage.ensurePrivateRoot(); err != nil {
		return nil, err
	}
	service := &EvidenceService{
		Storage: storage, Scanner: ClamAVScanner{Address: scanner}, Keys: keys, ActiveKeyID: keyID, MaxBytes: maxBytes,
		MaxPerReport: maxItems, AllowedMediaType: allowed,
		RetentionDays: retentionDays,
	}
	return service, nil
}

func (s *FilesystemEvidenceStorage) ensurePrivateRoot() error {
	if s == nil || s.Root == "" {
		return ErrEvidenceUnavailable
	}
	if err := os.MkdirAll(s.Root, 0700); err != nil {
		return fmt.Errorf("create private evidence root: %w", err)
	}
	info, err := os.Lstat(s.Root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("evidence storage root must be a real directory, not a symlink")
	}
	if err = os.Chmod(s.Root, 0700); err != nil {
		return fmt.Errorf("secure private evidence root: %w", err)
	}
	return nil
}

func (s *FilesystemEvidenceStorage) objectPath(id string) (string, error) {
	if len(id) != 32 {
		return "", errors.New("invalid evidence identifier")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return "", errors.New("invalid evidence identifier")
	}
	return filepath.Join(s.Root, id+".enc"), nil
}

func (s *FilesystemEvidenceStorage) Put(ctx context.Context, id string, data []byte) error {
	path, err := s.objectPath(id)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return writeErr
	}
	if syncErr != nil {
		_ = os.Remove(path)
		return syncErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return closeErr
	}
	return nil
}

func (s *FilesystemEvidenceStorage) Get(ctx context.Context, id string, maxBytes int64) ([]byte, error) {
	path, err := s.objectPath(id)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, maxBytes+4096))
}

func (s *FilesystemEvidenceStorage) Delete(ctx context.Context, id string) error {
	path, err := s.objectPath(id)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

func (s *EvidenceService) put(ctx context.Context, id string, data []byte) error {
	if s == nil || s.Storage == nil {
		return ErrEvidenceUnavailable
	}
	return s.Storage.Put(ctx, id, data)
}

func (s *EvidenceService) get(ctx context.Context, id string) ([]byte, error) {
	if s == nil || s.Storage == nil {
		return nil, ErrEvidenceUnavailable
	}
	return s.Storage.Get(ctx, id, s.MaxBytes)
}

func (s *EvidenceService) remove(ctx context.Context, id string) {
	if s != nil && s.Storage != nil {
		_ = s.Storage.Delete(ctx, id)
	}
}

func (s *EvidenceService) encrypt(id, mediaType string, plaintext []byte) ([]byte, error) {
	key, ok := s.Keys[s.ActiveKeyID]
	if !ok || len(key) != 32 {
		return nil, errors.New("active evidence encryption key is unavailable")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	if len(s.ActiveKeyID) > 64 {
		return nil, errors.New("invalid evidence key ID")
	}
	header := append([]byte("EVD1"), byte(len(s.ActiveKeyID)))
	header = append(header, []byte(s.ActiveKeyID)...)
	header = append(header, nonce...)
	aad := []byte(id + "|" + mediaType + "|" + s.ActiveKeyID)
	return gcm.Seal(header, nonce, plaintext, aad), nil
}

func (s *EvidenceService) decrypt(id, mediaType string, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < 5 || string(ciphertext[:4]) != "EVD1" {
		return nil, errors.New("invalid evidence envelope")
	}
	keyIDLength := int(ciphertext[4])
	if keyIDLength == 0 || keyIDLength > 64 || len(ciphertext) < 5+keyIDLength {
		return nil, errors.New("invalid evidence key identifier")
	}
	keyID := string(ciphertext[5 : 5+keyIDLength])
	key, ok := s.Keys[keyID]
	if !ok || len(key) != 32 {
		return nil, errors.New("evidence decryption key is unavailable")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	offset := 5 + keyIDLength
	if len(ciphertext) < offset+gcm.NonceSize()+gcm.Overhead() {
		return nil, errors.New("truncated evidence envelope")
	}
	nonce := ciphertext[offset : offset+gcm.NonceSize()]
	aad := []byte(id + "|" + mediaType + "|" + keyID)
	return gcm.Open(nil, nonce, ciphertext[offset+gcm.NonceSize():], aad)
}

func writeEvidenceScanner(conn net.Conn, data []byte) error {
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

// scan uses ClamAV's clamd INSTREAM protocol. It deliberately has no permissive
// fallback: timeout, connection failure, malformed response or FOUND rejects upload.
func (s *EvidenceService) scan(ctx context.Context, data []byte) error {
	if s == nil || s.Scanner == nil {
		return ErrEvidenceUnavailable
	}
	return s.Scanner.Scan(ctx, data)
}

func (s ClamAVScanner) Scan(ctx context.Context, data []byte) error {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", s.Address)
	if err != nil {
		return fmt.Errorf("connect to malware scanner: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err = writeEvidenceScanner(conn, []byte("zINSTREAM\x00")); err != nil {
		return err
	}
	for offset := 0; offset < len(data); {
		end := offset + 64*1024
		if end > len(data) {
			end = len(data)
		}
		chunk := data[offset:end]
		var size [4]byte
		size[0] = byte(len(chunk) >> 24)
		size[1] = byte(len(chunk) >> 16)
		size[2] = byte(len(chunk) >> 8)
		size[3] = byte(len(chunk))
		if err = writeEvidenceScanner(conn, size[:]); err != nil {
			return err
		}
		if err = writeEvidenceScanner(conn, chunk); err != nil {
			return err
		}
		offset = end
	}
	if err = writeEvidenceScanner(conn, []byte{0, 0, 0, 0}); err != nil {
		return err
	}
	response, err := bufio.NewReader(io.LimitReader(conn, 1024)).ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasSuffix(strings.TrimSpace(response), "OK") {
		return fmt.Errorf("malware scanner rejected content")
	}
	return nil
}

// Evidence handlers are API-mediated. Original filenames and client Content-Type
// headers are intentionally ignored; media type is sniffed from the bytes.
func (h *Handler) serveIncidentEvidence(w http.ResponseWriter, r *http.Request) bool {
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) == 4 && parts[0] == "v1" && parts[1] == "incidents" && parts[2] != "" && parts[3] == "evidence" && r.Method == http.MethodGet {
		h.listIncidentEvidence(w, r, parts[2], false)
		return true
	}
	if len(parts) == 5 && parts[0] == "v1" && parts[1] == "operator" && parts[2] == "incidents" && parts[3] != "" && parts[4] == "evidence" && r.Method == http.MethodGet {
		h.listIncidentEvidence(w, r, parts[3], true)
		return true
	}
	if len(parts) == 4 && parts[0] == "v1" && parts[1] == "incidents" && parts[2] != "" && parts[3] == "evidence" && r.Method == http.MethodPost {
		h.uploadIncidentEvidence(w, r, parts[2])
		return true
	}
	if len(parts) == 5 && parts[0] == "v1" && parts[1] == "incidents" && parts[2] != "" && parts[3] == "evidence" && r.Method == http.MethodGet {
		h.downloadIncidentEvidence(w, r, parts[2], parts[4], false)
		return true
	}
	if len(parts) == 6 && parts[0] == "v1" && parts[1] == "operator" && parts[2] == "incidents" && parts[3] != "" && parts[4] == "evidence" && r.Method == http.MethodGet {
		h.downloadIncidentEvidence(w, r, parts[3], parts[5], true)
		return true
	}
	return false
}

func (h *Handler) evidenceAudit(ctx context.Context, tenantID, reportID, evidenceID, actorID, action, outcome, reason string) error {
	if tenantID == "" {
		return errors.New("tenant is required for evidence audit")
	}
	_, err := h.Store.DB.Exec(ctx, `
		INSERT INTO incident_evidence_access_events
			(tenant_id,report_id,evidence_id,actor_identity_id,action,outcome,reason_category)
		VALUES($1,$2,$3,$4,$5,$6,$7)
	`, tenantID, reportID, evidenceID, actorID, action, outcome, reason)
	return err
}

func (h *Handler) evidenceDenied(w http.ResponseWriter, ctx context.Context, tenantID, reportID, evidenceID, actorID, action, reason string, status int) {
	if err := h.evidenceAudit(ctx, tenantID, reportID, evidenceID, actorID, action, "denied", reason); err != nil {
		writeError(w, http.StatusInternalServerError, "evidence access audit unavailable")
		return
	}
	writeError(w, status, "evidence access denied")
}

func (h *Handler) listIncidentEvidence(w http.ResponseWriter, r *http.Request, reportID string, reviewerOnly bool) {
	principal, err := h.principal(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if reportID == "" || len(reportID) > 200 {
		h.evidenceDenied(w, r.Context(), h.DefaultTenantID, reportID, "", principal.IdentityID, "list", "not_found", http.StatusNotFound)
		return
	}
	tx, err := h.Store.DB.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not authorize evidence listing")
		return
	}
	defer tx.Rollback(r.Context())
	tenantID := h.DefaultTenantID
	authorized := false
	if reviewerOnly {
		actor, actorErr := h.actorForRole(r, RoleOperator)
		if actorErr == nil {
			tenantID = actor.TenantID
			if authorizeOperatorTx(r.Context(), tx, actor, Resource{TenantID: actor.TenantID}, ActionIncidentReview) == nil {
				var exists bool
				if queryErr := tx.QueryRow(r.Context(), `
					SELECT EXISTS(SELECT 1 FROM incident_reports WHERE id=$1 AND tenant_id=$2)
				`, reportID, tenantID).Scan(&exists); queryErr == nil && exists {
					authorized = true
				}
			}
		}
	} else {
		for _, role := range []Role{RoleCustomer, RoleProfessional} {
			actor, actorErr := h.Store.ResolveActor(r.Context(), principal.IdentityID, h.DefaultTenantID, role)
			if actorErr != nil {
				continue
			}
			var exists bool
			queryErr := tx.QueryRow(r.Context(), `
				SELECT EXISTS(SELECT 1 FROM incident_reports
				WHERE id=$1 AND tenant_id=$2 AND reporter_identity_id=$3 AND reporter_role=$4)
			`, reportID, actor.TenantID, principal.IdentityID, string(role)).Scan(&exists)
			if queryErr == nil && exists {
				tenantID = actor.TenantID
				authorized = true
				break
			}
		}
	}
	if !authorized {
		_ = tx.Rollback(r.Context())
		h.evidenceDenied(w, r.Context(), tenantID, reportID, "", principal.IdentityID, "list", "unauthorized", http.StatusForbidden)
		return
	}
	rows, err := tx.Query(r.Context(), `
		SELECT id,media_type,size_bytes,created_at,retention_expires_at,legal_hold
		FROM incident_evidence WHERE report_id=$1 AND tenant_id=$2
		ORDER BY created_at,id
	`, reportID, tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list private evidence")
		return
	}
	items := make([]incidentEvidenceMetadata, 0)
	for rows.Next() {
		var item incidentEvidenceMetadata
		if err = rows.Scan(&item.ID, &item.MediaType, &item.SizeBytes, &item.CreatedAt, &item.RetentionUntil, &item.LegalHold); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "could not list private evidence")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		writeError(w, http.StatusInternalServerError, "could not list private evidence")
		return
	}
	rows.Close()
	if _, err = tx.Exec(r.Context(), `
		INSERT INTO incident_evidence_access_events
			(tenant_id,report_id,evidence_id,actor_identity_id,action,outcome,reason_category)
		VALUES($1,$2,'',$3,'list','success','accepted')
	`, tenantID, reportID, principal.IdentityID); err != nil {
		writeError(w, http.StatusInternalServerError, "evidence access audit unavailable")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "evidence access audit unavailable")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"evidence": items})
}

func (h *Handler) uploadIncidentEvidence(w http.ResponseWriter, r *http.Request, reportID string) {
	principal, err := h.principal(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Evidence == nil {
		writeError(w, http.StatusServiceUnavailable, "private evidence storage is not configured")
		return
	}
	if reportID == "" || len(reportID) > 200 {
		writeError(w, http.StatusNotFound, "incident report not found")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.Evidence.MaxBytes+1))
	if err != nil {
		h.evidenceDenied(w, r.Context(), h.DefaultTenantID, reportID, "", principal.IdentityID, "upload", "too_large", http.StatusRequestEntityTooLarge)
		return
	}
	if len(body) == 0 || int64(len(body)) > h.Evidence.MaxBytes {
		h.evidenceDenied(w, r.Context(), h.DefaultTenantID, reportID, "", principal.IdentityID, "upload", "too_large", http.StatusRequestEntityTooLarge)
		return
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(body), ";")[0]))
	if !h.Evidence.AllowedMediaType[mediaType] {
		h.evidenceDenied(w, r.Context(), h.DefaultTenantID, reportID, "", principal.IdentityID, "upload", "invalid_content", http.StatusUnsupportedMediaType)
		return
	}
	var actor Actor
	authorized := false
	for _, role := range []Role{RoleCustomer, RoleProfessional} {
		candidate, resolveErr := h.Store.ResolveActor(r.Context(), principal.IdentityID, h.DefaultTenantID, role)
		if resolveErr != nil {
			continue
		}
		var owner bool
		var status string
		queryErr := h.Store.DB.QueryRow(r.Context(), `
			SELECT reporter_identity_id=$3 AND reporter_role=$4,status
			FROM incident_reports WHERE id=$1 AND tenant_id=$2
		`, reportID, candidate.TenantID, principal.IdentityID, string(role)).Scan(&owner, &status)
		if queryErr == nil && owner && status != "closed" {
			actor, authorized = candidate, true
			break
		}
	}
	if !authorized {
		h.evidenceDenied(w, r.Context(), h.DefaultTenantID, reportID, "", principal.IdentityID, "upload", "unauthorized", http.StatusForbidden)
		return
	}
	scanCtx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	err = h.Evidence.scan(scanCtx, body)
	cancel()
	if err != nil {
		reason := "scanner_unavailable"
		if strings.Contains(err.Error(), "rejected") {
			reason = "scanner_rejected"
		}
		h.evidenceDenied(w, r.Context(), actor.TenantID, reportID, "", actor.SubjectID, "upload", reason, http.StatusServiceUnavailable)
		return
	}
	id, err := newID()
	if err != nil {
		h.evidenceDenied(w, r.Context(), actor.TenantID, reportID, "", actor.SubjectID, "upload", "storage_failure", http.StatusInternalServerError)
		return
	}
	encrypted, err := h.Evidence.encrypt(id, mediaType, body)
	if err != nil {
		h.evidenceDenied(w, r.Context(), actor.TenantID, reportID, id, actor.SubjectID, "upload", "key_failure", http.StatusServiceUnavailable)
		return
	}
	if err = h.Evidence.put(r.Context(), id, encrypted); err != nil {
		h.evidenceDenied(w, r.Context(), actor.TenantID, reportID, id, actor.SubjectID, "upload", "storage_failure", http.StatusServiceUnavailable)
		return
	}
	tx, err := h.Store.DB.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		h.Evidence.remove(r.Context(), id)
		writeError(w, http.StatusInternalServerError, "could not persist evidence metadata")
		return
	}
	defer tx.Rollback(r.Context())
	var currentStatus string
	var currentReporter string
	if err = tx.QueryRow(r.Context(), `
		SELECT status,reporter_identity_id FROM incident_reports
		WHERE id=$1 AND tenant_id=$2 FOR UPDATE
	`, reportID, actor.TenantID).Scan(&currentStatus, &currentReporter); err != nil ||
		currentReporter != actor.SubjectID || (currentStatus != "submitted" && currentStatus != "triaged" && currentStatus != "investigating") {
		h.Evidence.remove(r.Context(), id)
		h.evidenceDenied(w, r.Context(), actor.TenantID, reportID, id, actor.SubjectID, "upload", "unauthorized", http.StatusConflict)
		return
	}
	var count int
	if err = tx.QueryRow(r.Context(), `
		SELECT jsonb_array_length(r.evidence_refs) + count(e.id)
		FROM incident_reports r LEFT JOIN incident_evidence e
		  ON e.report_id=r.id AND e.tenant_id=r.tenant_id
		WHERE r.id=$1 AND r.tenant_id=$2
		GROUP BY r.id,r.evidence_refs
	`, reportID, actor.TenantID).Scan(&count); err != nil || count >= h.Evidence.MaxPerReport {
		h.Evidence.remove(r.Context(), id)
		h.evidenceDenied(w, r.Context(), actor.TenantID, reportID, id, actor.SubjectID, "upload", "limit_reached", http.StatusConflict)
		return
	}
	var retentionUntil *time.Time
	if h.Evidence.RetentionDays > 0 {
		expiry := time.Now().UTC().Add(time.Duration(h.Evidence.RetentionDays) * 24 * time.Hour)
		retentionUntil = &expiry
	}
	tag, err := tx.Exec(r.Context(), `
		INSERT INTO incident_evidence(id,report_id,tenant_id,reporter_identity_id,media_type,size_bytes,retention_expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7)
	`, id, reportID, actor.TenantID, actor.SubjectID, mediaType, len(body), retentionUntil)
	if err != nil || tag.RowsAffected() != 1 {
		h.Evidence.remove(r.Context(), id)
		h.evidenceDenied(w, r.Context(), actor.TenantID, reportID, id, actor.SubjectID, "upload", "storage_failure", http.StatusConflict)
		return
	}
	auditTag, auditErr := tx.Exec(r.Context(), `
		INSERT INTO incident_evidence_access_events
			(tenant_id,report_id,evidence_id,actor_identity_id,action,outcome,reason_category)
		VALUES($1,$2,$3,$4,'upload','success','accepted')
	`, actor.TenantID, reportID, id, actor.SubjectID)
	if auditErr != nil || auditTag.RowsAffected() != 1 {
		h.Evidence.remove(r.Context(), id)
		writeError(w, http.StatusInternalServerError, "evidence access audit unavailable")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not commit evidence metadata")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{"evidence_id": id, "media_type": mediaType, "size_bytes": len(body)})
}

func (h *Handler) downloadIncidentEvidence(w http.ResponseWriter, r *http.Request, reportID, evidenceID string, reviewerOnly bool) {
	principal, err := h.principal(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Evidence == nil {
		writeError(w, http.StatusServiceUnavailable, "private evidence storage is not configured")
		return
	}
	if len(reportID) == 0 || len(reportID) > 200 || len(evidenceID) != 32 {
		h.evidenceDenied(w, r.Context(), h.DefaultTenantID, reportID, evidenceID, principal.IdentityID, "download", "not_found", http.StatusNotFound)
		return
	}
	var tenantID string
	var mediaType string
	var expiry *time.Time
	var legalHold bool
	tx, err := h.Store.DB.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not authorize evidence access")
		return
	}
	defer tx.Rollback(r.Context())
	var authorized bool
	if reviewerOnly {
		actor, actorErr := h.actorForRole(r, RoleOperator)
		if actorErr == nil {
			authorized = authorizeOperatorTx(r.Context(), tx, actor, Resource{TenantID: actor.TenantID}, ActionIncidentReview) == nil
			tenantID = actor.TenantID
		}
	} else {
		for _, role := range []Role{RoleCustomer, RoleProfessional} {
			actor, actorErr := h.Store.ResolveActor(r.Context(), principal.IdentityID, h.DefaultTenantID, role)
			if actorErr != nil {
				continue
			}
			var owner bool
			queryErr := tx.QueryRow(r.Context(), `
				SELECT reporter_identity_id=$4 AND reporter_role=$5
				FROM incident_reports WHERE id=$1 AND tenant_id=$2 AND reporter_identity_id=$3
				FOR SHARE
			`, reportID, actor.TenantID, principal.IdentityID, principal.IdentityID, string(role)).Scan(&owner)
			if queryErr == nil && owner {
				authorized = true
				tenantID = actor.TenantID
				break
			}
		}
	}
	if !authorized {
		_ = tx.Rollback(r.Context())
		h.evidenceDenied(w, r.Context(), h.DefaultTenantID, reportID, evidenceID, principal.IdentityID, "download", "unauthorized", http.StatusForbidden)
		return
	}
	err = tx.QueryRow(r.Context(), `
		SELECT e.tenant_id,e.media_type,e.retention_expires_at,e.legal_hold
		FROM incident_evidence e
		JOIN incident_reports r ON r.id=e.report_id AND r.tenant_id=e.tenant_id
		WHERE e.id=$1 AND e.report_id=$2 AND e.tenant_id=$3
		FOR SHARE OF e,r
	`, evidenceID, reportID, tenantID).Scan(&tenantID, &mediaType, &expiry, &legalHold)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(r.Context())
		h.evidenceDenied(w, r.Context(), tenantID, reportID, evidenceID, principal.IdentityID, "download", "not_found", http.StatusNotFound)
		return
	}
	if err != nil {
		_ = tx.Rollback(r.Context())
		writeError(w, http.StatusInternalServerError, "could not load evidence metadata")
		return
	}
	if expiry != nil && time.Now().UTC().After(*expiry) && !legalHold {
		_ = tx.Rollback(r.Context())
		h.evidenceDenied(w, r.Context(), tenantID, reportID, evidenceID, principal.IdentityID, "download", "retention_expired", http.StatusGone)
		return
	}
	encrypted, err := h.Evidence.get(r.Context(), evidenceID)
	if err != nil {
		_ = tx.Rollback(r.Context())
		h.evidenceDenied(w, r.Context(), tenantID, reportID, evidenceID, principal.IdentityID, "download", "storage_failure", http.StatusNotFound)
		return
	}
	plaintext, err := h.Evidence.decrypt(evidenceID, mediaType, encrypted)
	if err != nil {
		_ = tx.Rollback(r.Context())
		h.evidenceDenied(w, r.Context(), tenantID, reportID, evidenceID, principal.IdentityID, "download", "key_failure", http.StatusServiceUnavailable)
		return
	}
	_, err = tx.Exec(r.Context(), `
		INSERT INTO incident_evidence_access_events
			(tenant_id,report_id,evidence_id,actor_identity_id,action,outcome,reason_category)
		VALUES($1,$2,$3,$4,'download','success','accepted')
	`, tenantID, reportID, evidenceID, principal.IdentityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "evidence access audit unavailable")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "evidence access audit unavailable")
		return
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Disposition", "attachment; filename=\"incident-evidence\"")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, bytes.NewReader(plaintext))
}

