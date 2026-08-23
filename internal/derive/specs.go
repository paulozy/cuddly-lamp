package derive

import (
	"encoding/json"
	"path"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
)

// Rule ids for API discovery. Two groups on purpose, with different confidence,
// because the two groups are not equally trustworthy — see SniffSpec.
const (
	// RuleAPIRootMarker is a spec that declared what it is. OpenAPI and AsyncAPI
	// both REQUIRE a root version field, so its presence is decisive.
	RuleAPIRootMarker = "api.root_marker"
	// RuleAPIExtensionSniff is a file whose kind was inferred from its extension
	// plus a content probe, because the format has no root marker at all.
	RuleAPIExtensionSniff = "api.extension_sniff"
)

// SpecKind mirrors models.APIKind without importing it: derive stays a pure
// package with no dependency on the persistence layer.
type SpecKind string

const (
	SpecOpenAPI  SpecKind = "openapi"
	SpecAsyncAPI SpecKind = "asyncapi"
	SpecGraphQL  SpecKind = "graphql"
	SpecGRPC     SpecKind = "grpc"
)

// Spec is one discovered API contract.
type Spec struct {
	Path string   `json:"path"`
	Kind SpecKind `json:"kind"`
	// Title and Version are display attributes. Neither is part of the identity;
	// see migration 034.
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
	// OperationCount is nil when the count could not be trusted — a spec whose
	// `paths` come in by `$ref` from another file yields an incomplete number, and
	// a confident zero would be worse than no number.
	OperationCount *int `json:"operation_count,omitempty"`
	// RuleID says which of the two detection groups fired, so the difference in
	// trustworthiness stays visible all the way to the drawer.
	RuleID string `json:"rule_id"`
	// Confidence is the fixed tier for RuleID. Never computed.
	Confidence float64 `json:"confidence"`
}

// APIsFact is the payload of an `apis` fact.
type APIsFact struct {
	Specs []Spec `json:"specs,omitempty"`
	// CandidateCount is how many paths matched the glob before sniffing, for the
	// evaluation of how noisy the shortlist is in practice.
	CandidateCount int `json:"candidate_count"`
}

// specHead is the shallow slice of a spec document that decides identity.
//
// Three fields and a decoder are enough, and that is the whole argument against
// an OpenAPI library here. `kin-openapi` resolves `$ref` and *validates on load*,
// which is work we do not want and which fails on a spec that is valid but
// incomplete. `libopenapi` keeps a full AST with line and column positions — it
// is built for low-level tooling and pays for itself the day there is a feature
// that renders or diffs an API doc, and not a day before.
type specHead struct {
	OpenAPI  string `yaml:"openapi" json:"openapi"`
	Swagger  string `yaml:"swagger" json:"swagger"`
	AsyncAPI string `yaml:"asyncapi" json:"asyncapi"`
	Info     struct {
		Title   string `yaml:"title" json:"title"`
		Version string `yaml:"version" json:"version"`
	} `yaml:"info" json:"info"`
	Paths    map[string]map[string]any `yaml:"paths" json:"paths"`
	Channels map[string]any            `yaml:"channels" json:"channels"`
	// Components is read only to notice `$ref` usage; its contents are ignored.
	Components map[string]any `yaml:"components" json:"components"`
}

// specGlobs are the paths worth reading. There is no official convention — the
// only thing that exists is the OpenAPI spec's own recommendation that "the entry
// document of an OAD be named `openapi.json` or `openapi.yaml`". There is no
// registry and no `.well-known` for repositories, and Redocly and Spectral define
// no default globs either (they read `redocly.yaml`).
//
// So the file name is a *filter*, never the decision. What decides is the content
// sniff in SniffSpec.
func isSpecCandidate(filePath string) bool {
	base := strings.ToLower(path.Base(filePath))
	ext := strings.ToLower(path.Ext(base))
	stem := strings.TrimSuffix(base, ext)

	switch ext {
	case ".proto":
		return true
	case ".graphql", ".graphqls", ".gql":
		return true
	case ".yaml", ".yml", ".json":
		switch stem {
		case "openapi", "swagger", "asyncapi", "api":
			return true
		}
		// A spec directory is the other common layout: `docs/api/orders.yaml`,
		// `openapi/v1/service.yaml`.
		dir := strings.ToLower(path.Dir(filePath))
		for _, segment := range strings.Split(dir, "/") {
			if segment == "openapi" || segment == "asyncapi" || segment == "apis" {
				return true
			}
		}
		return strings.HasSuffix(dir, "docs/api") || strings.HasSuffix(dir, "doc/api")
	default:
		return false
	}
}

// IsSpecCandidate reports whether a path is worth reading as an API spec.
func IsSpecCandidate(filePath string) bool { return isSpecCandidate(filePath) }

var (
	// protoService matches a protobuf service declaration. A .proto with only
	// messages is a data schema, not an API.
	protoService = regexp.MustCompile(`(?m)^\s*service\s+\w+\s*\{`)
	// graphqlRootType matches a GraphQL SDL root operation type. An SDL file
	// with only object types is a fragment of a schema, not an entry point.
	graphqlRootType = regexp.MustCompile(`(?m)^\s*(?:extend\s+)?type\s+(Query|Mutation|Subscription)\b`)
)

// SniffSpec decides what a candidate file actually is, or that it is not an API.
//
// The asymmetry between the two detection groups is deliberate and is why they
// carry different rule ids:
//
//   - OpenAPI and AsyncAPI REQUIRE a root version field (`openapi:` /
//     `asyncapi:`) and a REQUIRED Info Object with `title` and `version`. Their
//     presence is decisive, and their *absence* is equally decisive: a YAML file
//     with `paths` but no `info` is a referenced fragment, not an entry document,
//     and creating an API from it would put a half-contract in the catalog.
//   - GraphQL SDL and protobuf have no root marker at all. The only available
//     signal is the extension plus a probe for an entry point, which is a weaker
//     claim — hence the lower tier.
//
// Swagger 2.0 counts as `openapi`: it is the same contract in an older spelling,
// the field is `swagger` rather than `openapi`, and a catalog that dropped every
// v2 spec would be missing exactly the older services people most need to find.
func SniffSpec(filePath string, content []byte) (Spec, bool) {
	ext := strings.ToLower(path.Ext(filePath))
	switch ext {
	case ".proto":
		if !protoService.Match(content) {
			return Spec{}, false
		}
		return Spec{
			Path: filePath, Kind: SpecGRPC,
			Title:      strings.TrimSuffix(path.Base(filePath), ext),
			RuleID:     RuleAPIExtensionSniff,
			Confidence: ConfidenceExtensionSniff,
		}, true
	case ".graphql", ".graphqls", ".gql":
		if !graphqlRootType.Match(content) {
			return Spec{}, false
		}
		return Spec{
			Path: filePath, Kind: SpecGraphQL,
			Title:      strings.TrimSuffix(path.Base(filePath), ext),
			RuleID:     RuleAPIExtensionSniff,
			Confidence: ConfidenceExtensionSniff,
		}, true
	}

	head, ok := decodeSpecHead(filePath, content)
	if !ok {
		return Spec{}, false
	}

	spec := Spec{
		Path:       filePath,
		Title:      strings.TrimSpace(head.Info.Title),
		Version:    strings.TrimSpace(head.Info.Version),
		RuleID:     RuleAPIRootMarker,
		Confidence: ConfidenceExactName,
	}
	switch {
	case head.OpenAPI != "" || head.Swagger != "":
		spec.Kind = SpecOpenAPI
		spec.OperationCount = countOperations(head.Paths, content)
	case head.AsyncAPI != "":
		spec.Kind = SpecAsyncAPI
		spec.OperationCount = countChannels(head.Channels, content)
	default:
		// No root marker: this is not an entry document, whatever else it holds.
		return Spec{}, false
	}
	// The Info Object is REQUIRED and its `title` and `version` are REQUIRED
	// within it, so a document missing them is a fragment that happens to carry a
	// version field, not a contract.
	if spec.Title == "" || spec.Version == "" {
		return Spec{}, false
	}
	return spec, true
}

// decodeSpecHead reads a spec's head as either YAML or JSON.
//
// go-yaml handles both, because JSON is a YAML subset — one code path, so a JSON
// spec and a YAML spec cannot drift apart in how they are read.
func decodeSpecHead(filePath string, content []byte) (specHead, bool) {
	var head specHead
	if strings.EqualFold(path.Ext(filePath), ".json") {
		if err := json.Unmarshal(content, &head); err != nil {
			return specHead{}, false
		}
		return head, true
	}
	if err := yaml.Unmarshal(content, &head); err != nil {
		return specHead{}, false
	}
	return head, true
}

// httpMethods are the keys of a Path Item Object that are operations. Anything
// else in there (`parameters`, `summary`, `$ref`, `servers`) is not.
var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// countOperations counts the operations a spec declares inline, and returns nil
// when it cannot be trusted.
//
// A spec that pulls its paths in by `$ref` from another file yields a number that
// is real but incomplete, and "3 operations" for a service with thirty is worse
// than no number: it looks measured. So the presence of a `$ref` anywhere in the
// document withdraws the count entirely, and the UI renders a dash.
func countOperations(paths map[string]map[string]any, content []byte) *int {
	if paths == nil {
		return nil
	}
	if hasExternalRef(content) {
		return nil
	}
	total := 0
	for _, item := range paths {
		for key := range item {
			if httpMethods[strings.ToLower(key)] {
				total++
			}
		}
	}
	return &total
}

func countChannels(channels map[string]any, content []byte) *int {
	if channels == nil {
		return nil
	}
	if hasExternalRef(content) {
		return nil
	}
	total := len(channels)
	return &total
}

// hasExternalRef reports whether the document references another file.
//
// An internal `$ref` (`#/components/schemas/Order`) does not affect the operation
// count; a `$ref` to a path outside the document does. Distinguishing them
// without a resolver means looking for a `$ref` whose target does not start with
// `#`, which is exactly the cheap test that keeps this library-free.
func hasExternalRef(content []byte) bool {
	text := string(content)
	for idx := 0; ; {
		found := strings.Index(text[idx:], "$ref")
		if found < 0 {
			return false
		}
		idx += found + len("$ref")
		rest := strings.TrimLeft(text[idx:], "\"' \t:")
		if rest == "" {
			return false
		}
		if !strings.HasPrefix(rest, "#") {
			return true
		}
	}
}
