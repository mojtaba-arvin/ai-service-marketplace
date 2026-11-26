package marketplace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func testEvidenceService(t *testing.T) *EvidenceService {
	t.Helper()
	root := filepath.Join(t.TempDir(), "private-evidence")
	service := &EvidenceService{
		Storage: &FilesystemEvidenceStorage{Root: root},
		Scanner: ClamAVScanner{Address: "127.0.0.1:1"},
		Keys: map[string][]byte{
			"key-v1": []byte("0123456789abcdef0123456789abcdef"),
			"key-v2": []byte("abcdef0123456789abcdef0123456789"),
		},
		ActiveKeyID: "key-v2",
		MaxBytes: 1 << 20,
		MaxPerReport: 5,
		AllowedMediaType: map[string]bool{"application/pdf": true, "image/png": true},
		ScannerAddress: "127.0.0.1:1",
	}
	if err := service.Storage.(*FilesystemEvidenceStorage).ensurePrivateRoot(); err != nil {
		t.Fatal(err)
	}
	return service
}

func TestEvidenceEncryptionAndKeyRotation(t *testing.T) {
	service := testEvidenceService(t)
	plaintext := []byte("%PDF-1.7\nprivate incident evidence")
	encrypted, err := service.encrypt("0123456789abcdef0123456789abcdef", "application/pdf", plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if string(encrypted[:4]) != "EVD1" {
		t.Fatal("encrypted evidence envelope has no version marker")
	}
	rotated := *service
	rotated.Keys = map[string][]byte{"key-v1": service.Keys["key-v1"], "key-v2": service.Keys["key-v2"]}
	rotated.ActiveKeyID = "key-v1"
	decrypted, err := rotated.decrypt("0123456789abcdef0123456789abcdef", "application/pdf", encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if string(decrypted) != string(plaintext) {
		t.Fatal("decrypted evidence differs from original")
	}
	if _, err = service.decrypt("fedcba9876543210fedcba9876543210", "application/pdf", encrypted); err == nil {
		t.Fatal("evidence identifier must be authenticated as associated data")
	}
	if _, err = service.decrypt("0123456789abcdef0123456789abcdef", "image/png", encrypted); err == nil {
		t.Fatal("media type must be authenticated as associated data")
	}
}

func TestEvidenceFilesystemUsesPrivatePermissionsAndOpaqueNames(t *testing.T) {
	service := testEvidenceService(t)
	id := "0123456789abcdef0123456789abcdef"
	if err := service.put(context.Background(), id, []byte("ciphertext")); err != nil {
		t.Fatal(err)
	}
	storage := service.Storage.(*FilesystemEvidenceStorage)
	path := filepath.Join(storage.Root, id+".enc")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("evidence object permissions = %o, want 600", info.Mode().Perm())
	}
	rootInfo, err := os.Stat(storage.Root)
	if err != nil {
		t.Fatal(err)
	}
	if rootInfo.Mode().Perm() != 0700 {
		t.Fatalf("evidence root permissions = %o, want 700", rootInfo.Mode().Perm())
	}
	got, err := service.get(context.Background(), id)
	if err != nil || string(got) != "ciphertext" {
		t.Fatalf("read private evidence: %q, %v", got, err)
	}
	if _, err = storage.objectPath("../outside"); err == nil {
		t.Fatal("path traversal identifier must be rejected")
	}
}

func TestEvidenceScannerFailsClosedWhenUnavailable(t *testing.T) {
	service := testEvidenceService(t)
	if err := service.scan(context.Background(), []byte("not scanned")); err == nil {
		t.Fatal("scanner outage must reject evidence")
	}
}

func TestEvidenceDecryptRejectsUnknownKey(t *testing.T) {
	service := testEvidenceService(t)
	encrypted, err := service.encrypt("0123456789abcdef0123456789abcdef", "application/pdf", []byte("data"))
	if err != nil {
		t.Fatal(err)
	}
	withoutOldKey := *service
	withoutOldKey.Keys = map[string][]byte{"key-v1": service.Keys["key-v1"]}
	if _, err = withoutOldKey.decrypt("0123456789abcdef0123456789abcdef", "application/pdf", encrypted); err == nil {
		t.Fatal("missing historical key must fail closed")
	}
}
