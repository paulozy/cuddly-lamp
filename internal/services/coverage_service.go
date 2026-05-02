package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/coverage"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	coverageTokenPrefix    = "cov_"
	coverageTokenRandBytes = 32
)

var (
	ErrCoverageInvalidFormat   = errors.New("unsupported coverage format")
	ErrCoverageInvalidSHA      = errors.New("invalid commit sha")
	ErrCoverageBodyTooLarge    = errors.New("coverage body exceeds size limit")
	ErrCoverageParseFailed     = errors.New("failed to parse coverage payload")
	ErrCoverageTokenInvalid    = errors.New("coverage upload token is invalid")
	ErrCoverageTokenForeignRepo = errors.New("coverage upload token does not match repository")
	ErrCoverageTokenExpired    = errors.New("coverage upload token expired or revoked")
	ErrCoverageTokenNotFound   = errors.New("coverage upload token not found")
)

// CoverageService handles ingest of CI-uploaded reports plus the lifecycle
// of upload tokens.
type CoverageService struct {
	repo storage.Repository
}

func NewCoverageService(repo storage.Repository) *CoverageService {
	return &CoverageService{repo: repo}
}

// IngestRequest carries the parsed inputs of a coverage upload HTTP request.
type IngestRequest struct {
	RepositoryID string
	Token        string
	Format       string
	CommitSHA    string
	Branch       string
	Body         io.Reader
}

// IngestCoverage validates the token, parses the body, persists the upload
// and best-effort patches the latest code_analyses row for the same SHA.
func (s *CoverageService) IngestCoverage(ctx context.Context, req IngestRequest) (*models.CoverageUpload, error) {
	if req.RepositoryID == "" {
		return nil, errors.New("repository_id is required")
	}
	if !isPlausibleSHA(req.CommitSHA) {
		return nil, ErrCoverageInvalidSHA
	}

	parser, format, ok := parserFor(req.Format)
	if !ok {
		return nil, ErrCoverageInvalidFormat
	}

	tokenModel, err := s.authenticateToken(ctx, req.RepositoryID, req.Token)
	if err != nil {
		return nil, err
	}

	limited := io.LimitReader(req.Body, coverage.MaxReportFileBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read coverage body: %w", err)
	}
	if len(raw) > coverage.MaxReportFileBytes {
		return nil, ErrCoverageBodyTooLarge
	}

	report, err := parser(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCoverageParseFailed, err)
	}

	status := coverage.StatusOK
	if report.LinesTotal == 0 {
		status = coverage.StatusFailed
	}
	percentage := 0.0
	if report.LinesTotal > 0 {
		percentage = 100.0 * float64(report.LinesCovered) / float64(report.LinesTotal)
	}

	files := make(map[string]coverage.FileCoverage, len(report.Files))
	for _, f := range report.Files {
		if f.Path == "" {
			continue
		}
		files[f.Path] = f
	}

	upload := &models.CoverageUpload{
		RepositoryID:      req.RepositoryID,
		CommitSHA:         req.CommitSHA,
		Branch:            req.Branch,
		Format:            format,
		LinesCovered:      report.LinesCovered,
		LinesTotal:        report.LinesTotal,
		Percentage:        percentage,
		Status:            status,
		RawSizeBytes:      len(raw),
		Files:             datatypes.NewJSONType(files),
		Warnings:          models.StringArray{},
		UploadedByTokenID: tokenModel.ID,
	}
	if err := s.repo.CreateCoverageUpload(ctx, upload); err != nil {
		return nil, fmt.Errorf("persist coverage upload: %w", err)
	}

	// Best-effort: patch the latest completed code_analyses row for this SHA.
	if affected, err := s.repo.PatchCodeAnalysisCoverage(ctx, req.RepositoryID, req.CommitSHA, report.LinesCovered, report.LinesTotal, percentage, string(status)); err != nil {
		utils.Warn("coverage service: patch analysis failed", "repository_id", req.RepositoryID, "sha", req.CommitSHA, "error", err)
	} else if affected == 0 {
		utils.Info("coverage service: no analysis to patch yet (will reconcile on next analysis)", "repository_id", req.RepositoryID, "sha", req.CommitSHA)
	}

	if err := s.repo.TouchCoverageUploadTokenUsage(ctx, tokenModel.ID); err != nil {
		utils.Warn("coverage service: token touch failed", "token_id", tokenModel.ID, "error", err)
	}

	return upload, nil
}

// LookupForAnalysis returns the most recent coverage upload for the given
// (repo, sha). Returns (nil, nil) when no upload is registered yet.
func (s *CoverageService) LookupForAnalysis(ctx context.Context, repoID, sha string) (*models.CoverageUpload, error) {
	if repoID == "" || sha == "" {
		return nil, nil
	}
	return s.repo.GetLatestCoverageUpload(ctx, repoID, sha)
}

// CreateUploadToken generates a fresh token, persists its hash, and returns
// the plaintext to the caller (shown ONCE).
// userID is optional — pass "" to leave created_by_user_id NULL.
func (s *CoverageService) CreateUploadToken(ctx context.Context, repoID, name, userID string, expiresAt *time.Time) (string, *models.CoverageUploadToken, error) {
	if repoID == "" || name == "" {
		return "", nil, errors.New("repository_id and name are required")
	}
	plain, err := generatePlaintextToken()
	if err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}
	hashed := hashCoverageToken(plain)
	model := &models.CoverageUploadToken{
		RepositoryID: repoID,
		Name:         name,
		TokenHash:    hashed,
		ExpiresAt:    expiresAt,
	}
	if userID != "" {
		model.CreatedByUserID = &userID
	}
	if err := s.repo.CreateCoverageUploadToken(ctx, model); err != nil {
		return "", nil, err
	}
	return plain, model, nil
}

// RevokeUploadToken marks the token as revoked. Returns ErrCoverageTokenNotFound
// when the id does not exist or the token belongs to a different repository.
func (s *CoverageService) RevokeUploadToken(ctx context.Context, repoID, id string) error {
	tok, err := s.repo.GetCoverageUploadToken(ctx, id)
	if err != nil {
		return err
	}
	if tok == nil || tok.RepositoryID != repoID {
		return ErrCoverageTokenNotFound
	}
	if err := s.repo.RevokeCoverageUploadToken(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrCoverageTokenNotFound
		}
		return err
	}
	return nil
}

// ListUploadTokens lists tokens for a repository (newest first).
func (s *CoverageService) ListUploadTokens(ctx context.Context, repoID string) ([]*models.CoverageUploadToken, error) {
	return s.repo.ListCoverageUploadTokens(ctx, repoID)
}

// authenticateToken validates the token string, scope, and active status.
func (s *CoverageService) authenticateToken(ctx context.Context, repoID, token string) (*models.CoverageUploadToken, error) {
	if !strings.HasPrefix(token, coverageTokenPrefix) {
		return nil, ErrCoverageTokenInvalid
	}
	hashed := hashCoverageToken(token)
	stored, err := s.repo.GetCoverageUploadTokenByHash(ctx, hashed)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, ErrCoverageTokenInvalid
	}
	if stored.RepositoryID != repoID {
		return nil, ErrCoverageTokenForeignRepo
	}
	if !stored.IsActive(time.Now().UTC()) {
		return nil, ErrCoverageTokenExpired
	}
	return stored, nil
}

func parserFor(format string) (func(io.Reader) (coverage.Report, error), coverage.Format, bool) {
	switch coverage.Format(format) {
	case coverage.FormatGo:
		return coverage.ParseGoCover, coverage.FormatGo, true
	case coverage.FormatLCOV:
		return coverage.ParseLCOV, coverage.FormatLCOV, true
	case coverage.FormatCobertura:
		return coverage.ParseCobertura, coverage.FormatCobertura, true
	case coverage.FormatJaCoCo:
		return coverage.ParseJaCoCo, coverage.FormatJaCoCo, true
	}
	return nil, "", false
}

func generatePlaintextToken() (string, error) {
	buf := make([]byte, coverageTokenRandBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return coverageTokenPrefix + hex.EncodeToString(buf), nil
}

func hashCoverageToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

func isPlausibleSHA(s string) bool {
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if !isHex {
			return false
		}
	}
	return true
}
