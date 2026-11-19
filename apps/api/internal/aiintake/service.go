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
	CategoryID string `json:"category_id"`
	Summary string `json:"summary"`
	RequiredDetails []string `json:"required_details"`
	Confidence float64 `json:"confidence"`
}

type Service struct {
	Catalog Catalog
	Model Model
	Timeout time.Duration
	Now func() time.Time
}

func (s Service) Suggest(ctx context.Context, request Request) (Suggestion, error) {
	if s.Catalog == nil || s.Model == nil || s.Timeout <= 0 ||
		strings.TrimSpace(request.Market) == "" || strings.TrimSpace(request.Locale) == "" ||
		strings.TrimSpace(request.Text) == "" || len(request.Text) > 8000 {
		return Suggestion{}, ErrInvalidRequest
	}
	categories, err := s.Catalog.EnabledCategories(ctx, request.Market, request.Locale)
	if err != nil { return Suggestion{}, fmt.Errorf("load enabled catalog categories: %w", err) }
	if len(categories) == 0 { return Suggestion{}, ErrInvalidRequest }
	allowed := make(map[string]struct{}, len(categories))
	for _, category := range categories {
		if strings.TrimSpace(category.ID) == "" { return Suggestion{}, ErrInvalidRequest }
		allowed[category.ID] = struct{}{}
	}

	modelCtx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	output, err := s.Model.Generate(modelCtx, ModelInput{
		Market: request.Market, Locale: request.Locale,
		Text: strings.TrimSpace(request.Text), AllowedCategories: categories,
	})
	if err != nil { return Suggestion{}, fmt.Errorf("generate intake suggestion: %w", err) }
	if len(output.JSON) == 0 || len(output.JSON) > 16*1024 ||
		strings.TrimSpace(output.Name) == "" || strings.TrimSpace(output.Version) == "" {
		return Suggestion{}, ErrInvalidSuggestion
	}

	decoder := json.NewDecoder(bytes.NewReader(output.JSON))
	decoder.DisallowUnknownFields()
	var candidate modelSuggestion
	if err := decoder.Decode(&candidate); err != nil { return Suggestion{}, fmt.Errorf("%w: %v", ErrInvalidSuggestion, err) }
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return Suggestion{}, fmt.Errorf("%w: trailing JSON content", ErrInvalidSuggestion)
	}
	if _, ok := allowed[candidate.CategoryID]; !ok ||
		strings.TrimSpace(candidate.Summary) == "" || len(candidate.Summary) > 1000 ||
		len(candidate.RequiredDetails) > 10 ||
		math.IsNaN(candidate.Confidence) || math.IsInf(candidate.Confidence, 0) ||
		candidate.Confidence < 0 || candidate.Confidence > 1 {
		return Suggestion{}, ErrInvalidSuggestion
	}
	for _, detail := range candidate.RequiredDetails {
		if strings.TrimSpace(detail) == "" || len(detail) > 160 { return Suggestion{}, ErrInvalidSuggestion }
	}

	fingerprint := sha256.Sum256([]byte(strings.TrimSpace(request.Text)))
	now := time.Now
	if s.Now != nil { now = s.Now }
	return Suggestion{
		CategoryID: candidate.CategoryID, Summary: strings.TrimSpace(candidate.Summary),
		RequiredDetails: candidate.RequiredDetails, Confidence: candidate.Confidence,
		Provenance: Provenance{
			Model: strings.TrimSpace(output.Name), ModelVersion: strings.TrimSpace(output.Version),
			InputSHA256: hex.EncodeToString(fingerprint[:]), GeneratedAt: now().UTC(),
		},
	}, nil
}
