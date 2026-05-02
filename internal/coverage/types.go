package coverage

// Format identifies the coverage report format produced by a CI run.
type Format string

const (
	FormatGo        Format = "go"
	FormatLCOV      Format = "lcov"
	FormatCobertura Format = "cobertura"
	FormatJaCoCo    Format = "jacoco"
)

// Status describes the outcome of producing a coverage report from CI input.
type Status string

const (
	StatusOK            Status = "ok"
	StatusNotConfigured Status = "not_configured"
	StatusPartial       Status = "partial"
	StatusFailed        Status = "failed"
)

// MaxReportFileBytes caps the size of a single coverage report ingested by
// the API. Larger reports are rejected by the upload handler with HTTP 413.
const MaxReportFileBytes = 5 * 1024 * 1024

// FileCoverage holds the coverage breakdown for a single source file.
// The PR rule consumes this map to flag newly added files that arrived
// without test coverage.
type FileCoverage struct {
	Path         string `json:"path"`
	LinesCovered int    `json:"lines_covered"`
	LinesTotal   int    `json:"lines_total"`
}

// Report is the parsed result of a single coverage payload.
type Report struct {
	Format       Format         `json:"format"`
	Path         string         `json:"path,omitempty"` // empty when produced from an upload (no path concept)
	LinesCovered int            `json:"lines_covered"`
	LinesTotal   int            `json:"lines_total"`
	Percentage   float64        `json:"percentage"`
	Files        []FileCoverage `json:"files,omitempty"`
}
