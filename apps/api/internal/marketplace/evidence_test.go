package marketplace

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
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
	path, err := storage.objectPath(id)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) == id+".enc" || len(filepath.Base(path)) != 68 {
		t.Fatalf("evidence path must use a fixed-length derived name, got %q", filepath.Base(path))
	}
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

func TestClamAVScannerUsesBoundedINSTREAMProtocol(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverErr := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverErr <- acceptErr
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		command, readErr := reader.ReadString('\x00')
		if readErr != nil {
			serverErr <- readErr
			return
		}
		if command != "zINSTREAM\x00" {
			serverErr <- io.ErrUnexpectedEOF
			return
		}
		for {
			var size [4]byte
			if _, readErr = io.ReadFull(reader, size[:]); readErr != nil {
				serverErr <- readErr
				return
			}
			length := binary.BigEndian.Uint32(size[:])
			if length == 0 {
				break
			}
			if _, readErr = io.CopyN(io.Discard, reader, int64(length)); readErr != nil {
				serverErr <- readErr
				return
			}
		}
		_, writeErr := io.WriteString(conn, "stream: OK\n")
		serverErr <- writeErr
	}()
	scanner := ClamAVScanner{Address: listener.Addr().String()}
	payload := bytes.Repeat([]byte("A"), 70*1024)
	if err = scanner.Scan(context.Background(), payload); err != nil {
		t.Fatalf("clamd scan: %v", err)
	}
	if err = <-serverErr; err != nil {
		t.Fatalf("clamd protocol server: %v", err)
	}
}

func TestClamAVScannerRejectsMalformedAndInfectedResponses(t *testing.T) {
	for _, response := range []string{"unexpected OK\n", "stream: Eicar-Test-Signature FOUND\n"} {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		serverErr := make(chan error, 1)
		go func(response string) {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				serverErr <- acceptErr
				return
			}
			defer conn.Close()
			reader := bufio.NewReader(conn)
			if _, readErr := reader.ReadString('\x00'); readErr != nil {
				serverErr <- readErr
				return
			}
			for {
				var size [4]byte
				if _, readErr := io.ReadFull(reader, size[:]); readErr != nil {
					serverErr <- readErr
					return
				}
				length := binary.BigEndian.Uint32(size[:])
				if length == 0 {
					break
				}
				if _, readErr := io.CopyN(io.Discard, reader, int64(length)); readErr != nil {
					serverErr <- readErr
					return
				}
			}
			_, writeErr := io.WriteString(conn, response)
			serverErr <- writeErr
		}(response)
		scanner := ClamAVScanner{Address: listener.Addr().String()}
		if err = scanner.Scan(context.Background(), []byte("test evidence")); err == nil {
			listener.Close()
			t.Fatalf("scanner response %q must fail closed", response)
		}
		if err = <-serverErr; err != nil {
			listener.Close()
			t.Fatalf("scanner response server: %v", err)
		}
		listener.Close()
	}
}
