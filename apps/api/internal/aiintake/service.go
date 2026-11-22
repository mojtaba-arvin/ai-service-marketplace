// Package aiintake defines a constrained, non-authoritative AI intake suggestion boundary.
package aiintake

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
)

var (
	ErrInvalidRequest = errors.New("invalid intake request")
	ErrInvalidSuggestion = errors.New("model returned an invalid intake suggestion")
)

type Request struct {
	Market string
	Locale string
	Text string
}

type Category struct {
	ID string
	Label string
}

type Catalog interface {
	EnabledCategories(context.Context, string, string) ([]Category, error)
}

type ModelInput struct {
	Market string
	Locale string
	Text string
	AllowedCategories []Category
}

type ModelOutput struct {
	JSON []byte
	Name string
	Version string
}

type Model interface {
	Generate(context.Context, ModelInput) (ModelOutput, error)
}

type Redactor interface {
	Redact(context.Context, string) (string, error)
}

type Suggestion struct {
	CategoryID string `json:"category_id"`
	Summary string `json:"summary"`
	RequiredDetails []string `json:"required_details"`
	Confidence float64 `json:"confidence"`
	Provenance Provenance `json:"provenance"`
}

type Provenance struct {
	Model string `json:"model"`
	ModelVersion string `json:"model_version"`
	InputSHA256 string `json:"input_sha256"`
	GeneratedAt time.Time `json:"generated_at"`
}

type modelSuggestion struct {
	CategoryID *string `json:"category_id"`
	Summary *string `json:"summary"`
	RequiredDetails *[]string `json:"required_details"`
	Confidence *float64 `json:"confidence"`
}

type Service struct {
	Catalog Catalog
	Model Model
	Redactor Redactor
	Timeout time.Duration
	Now func() time.Time
}

func validProviderMetadata(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func (s Service) Suggest(ctx context.Context, request Request) (Suggestion, error) {
	if s.Catalog == nil || s.Model == nil || s.Redactor == nil || s.Timeout <= 0 ||
		strings.TrimSpace(request.Market) == "" || strings.TrimSpace(request.Locale) == "" ||
		strings.TrimSpace(request.Text) == "" || len(request.Text) > 8000 {
		return Suggestion{}, ErrInvalidRequest
	}
	redactedText, err := s.Redactor.Redact(ctx, strings.TrimSpace(request.Text))
	if err != nil { return Suggestion{}, fmt.Errorf("redact intake text: %w", err) }
	redactedText = strings.TrimSpace(redactedText)
	if redactedText == "" || len(redactedText) > 8000 { return Suggestion{}, ErrInvalidRequest }
	categories, err := s.Catalog.EnabledCategories(ctx, request.Market, request.Locale)
	if err != nil { return Suggestion{}, fmt.Errorf("load enabled catalog categories: %w", err) }
	if len(categories) == 0 { return Suggestion{}, ErrInvalidRequest }
\tallowed := make(map[string]struct{}, len(categories))
\tfor _, category := range categories {
\t\tid := strings.TrimSpace(category.ID)
\t\tif id == "" || id != category.ID || len(id) > 128 ||
\t\t\tstrings.TrimSpace(category.Label) == "" || len(category.Label) > 160 {
\t\t\treturn Suggestion{}, ErrInvalidRequest
\t\t}
\t\tif _, exists := allowed[id]; exists {
\t\t\treturn Suggestion{}, ErrInvalidRequest
\t\t}
\t\tallowed[id] = struct{}{}
\t}

	modelCtx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	output, err := s.Model.Generate(modelCtx, ModelInput{
		Market: request.Market, Locale: request.Locale,
		Text: redactedText, AllowedCategories: categories,
	})
	if err != nil { return Suggestion{}, fmt.Errorf("generate intake suggestion: %w", err) }
	if err := modelCtx.Err(); err != nil { return Suggestion{}, err }
	if len(output.JSON) == 0 || len(output.JSON) > 16*1024 ||
		!validProviderMetadata(output.Name) || !validProviderMetadata(output.Version) {
		return Suggestion{}, ErrInvalidSuggestion
	}

	decoder := json.NewDecoder(bytes.NewReader(output.JSON))
	decoder.DisallowUnknownFields()
	var candidate modelSuggestion
	if err := decoder.Decode(&candidate); err != nil { return Suggestion{}, fmt.Errorf("%w: %v", ErrInvalidSuggestion, err) }
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return Suggestion{}, fmt.Errorf("%w: trailing JSON content", ErrInvalidSuggestion)
	}
	if candidate.CategoryID == nil || candidate.Summary == nil || candidate.RequiredDetails == nil || candidate.Confidence == nil {
		return Suggestion{}, ErrInvalidSuggestion
	}
	if _, ok := allowed[*candidate.CategoryID]; !ok ||
		strings.TrimSpace(*candidate.Summary) == "" || len(*candidate.Summary) > 1000 ||
		len(*candidate.RequiredDetails) > 10 ||
		math.IsNaN(*candidate.Confidence) || math.IsInf(*candidate.Confidence, 0) ||
		*candidate.Confidence < 0 || *candidate.Confidence > 1 {
		return Suggestion{}, ErrInvalidSuggestion
	}
	seenDetails := make(map[string]struct{}, len(*candidate.RequiredDetails))
	for _, detail := range *candidate.RequiredDetails {
		normalized := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(detail)), " "))
		if normalized == "" || len(detail) > 160 {
			return Suggestion{}, ErrInvalidSuggestion
		}
		if _, exists := seenDetails[normalized]; exists {
			return Suggestion{}, ErrInvalidSuggestion
		}
		seenDetails[normalized] = struct{}{}
	}

	fingerprint := sha256.Sum256([]byte(redactedText))
	now := time.Now
	if s.Now != nil { now = s.Now }
	return Suggestion{
		CategoryID: *candidate.CategoryID, Summary: strings.TrimSpace(*candidate.Summary),
		RequiredDetails: *candidate.RequiredDetails, Confidence: *candidate.Confidence,
		Provenance: Provenance{
			Model: strings.TrimSpace(output.Name), ModelVersion: strings.TrimSpace(output.Version),
			InputSHA256: hex.EncodeToString(fingerprint[:]), GeneratedAt: now().UTC(),
		},
	}, nil
}
