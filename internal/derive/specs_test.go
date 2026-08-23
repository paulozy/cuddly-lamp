package derive

import (
	"strings"
	"testing"
)

// ── the shortlist ────────────────────────────────────────────────────────────

// The file name is a filter, never the decision. There is no official path
// convention for API specs — no registry, no `.well-known` for repositories, and
// the only thing that exists is the OpenAPI spec's recommendation that the entry
// document be named `openapi.yaml`. So this table is about what is worth *reading*.
func TestIsSpecCandidate(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "openapi.yaml", want: true},
		{path: "openapi.json", want: true},
		{path: "swagger.yml", want: true},
		{path: "asyncapi.yaml", want: true},
		{path: "api.yaml", want: true},
		{path: "openapi/v1/orders.yaml", want: true},
		{path: "docs/api/orders.yaml", want: true},
		{path: "apis/orders/spec.yml", want: true},
		{path: "proto/orders.proto", want: true},
		{path: "schema.graphql", want: true},
		{path: "schema.graphqls", want: true},
		{path: "schema.gql", want: true},
		// A random YAML is not a candidate: reading every yaml in a repository to
		// find out would cost hundreds of requests per sync.
		{path: "docker-compose.yml", want: false},
		{path: "k8s/deployment.yaml", want: false},
		{path: ".github/workflows/ci.yml", want: false},
		{path: "package.json", want: false},
		{path: "README.md", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := IsSpecCandidate(tt.path); got != tt.want {
				t.Errorf("IsSpecCandidate(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// ── the sniff ────────────────────────────────────────────────────────────────

const openAPIYAML = `openapi: 3.0.3
info:
  title: Orders API
  version: 1.4.0
paths:
  /orders:
    get:
      summary: list
    post:
      summary: create
  /orders/{id}:
    get:
      summary: read
    parameters:
      - name: id
`

func TestSniffSpec_OpenAPI(t *testing.T) {
	spec, ok := SniffSpec("openapi.yaml", []byte(openAPIYAML))
	if !ok {
		t.Fatal("SniffSpec() ok = false, want true")
	}
	if spec.Kind != SpecOpenAPI {
		t.Errorf("kind = %q, want %q", spec.Kind, SpecOpenAPI)
	}
	if spec.Title != "Orders API" || spec.Version != "1.4.0" {
		t.Errorf("title/version = %q/%q, want Orders API/1.4.0", spec.Title, spec.Version)
	}
	// `parameters` sits alongside the methods in a Path Item Object and is not an
	// operation. Counting it would inflate every spec that declares one.
	if spec.OperationCount == nil || *spec.OperationCount != 3 {
		t.Errorf("operation_count = %v, want 3", spec.OperationCount)
	}
	// A root marker is a decisive claim, so it carries the top tier.
	if spec.RuleID != RuleAPIRootMarker {
		t.Errorf("rule_id = %q, want %q", spec.RuleID, RuleAPIRootMarker)
	}
}

// JSON and YAML go through one decoder, so a JSON spec and a YAML spec cannot
// drift apart in how they are read.
func TestSniffSpec_JSONAndYAMLTakeTheSamePath(t *testing.T) {
	jsonSpec := `{"openapi":"3.0.3","info":{"title":"Orders API","version":"1.4.0"},"paths":{"/orders":{"get":{}}}}`
	spec, ok := SniffSpec("openapi.json", []byte(jsonSpec))
	if !ok {
		t.Fatal("SniffSpec() ok = false, want true")
	}
	if spec.Kind != SpecOpenAPI || spec.Title != "Orders API" {
		t.Errorf("spec = %+v, want an openapi named Orders API", spec)
	}
}

// Swagger 2.0 is the same contract in an older spelling, and the field is
// `swagger` rather than `openapi`. A catalog that dropped v2 would be missing
// exactly the older services people most need to find.
func TestSniffSpec_Swagger2CountsAsOpenAPI(t *testing.T) {
	content := `swagger: "2.0"
info:
  title: Legacy API
  version: "1.0"
paths:
  /ping:
    get: {}
`
	spec, ok := SniffSpec("swagger.yaml", []byte(content))
	if !ok {
		t.Fatal("SniffSpec() ok = false, want true")
	}
	if spec.Kind != SpecOpenAPI {
		t.Errorf("kind = %q, want %q", spec.Kind, SpecOpenAPI)
	}
}

func TestSniffSpec_AsyncAPI(t *testing.T) {
	content := `asyncapi: 2.6.0
info:
  title: Orders Events
  version: 2.0.0
channels:
  order/created: {}
  order/shipped: {}
`
	spec, ok := SniffSpec("asyncapi.yaml", []byte(content))
	if !ok {
		t.Fatal("SniffSpec() ok = false, want true")
	}
	if spec.Kind != SpecAsyncAPI {
		t.Errorf("kind = %q, want %q", spec.Kind, SpecAsyncAPI)
	}
	if spec.OperationCount == nil || *spec.OperationCount != 2 {
		t.Errorf("operation_count = %v, want 2 channels", spec.OperationCount)
	}
}

// No root marker means this is not an entry document, whatever else it holds.
// Creating an API from a referenced fragment would put a half-contract in the
// catalog under a name nobody recognizes.
func TestSniffSpec_NoRootMarkerYieldsNoAPI(t *testing.T) {
	content := `info:
  title: Shared Schemas
  version: 1.0.0
components:
  schemas:
    Order: {}
`
	if _, ok := SniffSpec("openapi.yaml", []byte(content)); ok {
		t.Error("SniffSpec() ok = true, want false without a root openapi/asyncapi field")
	}
}

// The Info Object is REQUIRED and `title` and `version` are REQUIRED within it, so
// a document missing them is a fragment that happens to carry a version field.
func TestSniffSpec_MissingInfoYieldsNoAPI(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "no info at all", content: "openapi: 3.0.3\npaths:\n  /orders:\n    get: {}\n"},
		{name: "info without title", content: "openapi: 3.0.3\ninfo:\n  version: 1.0.0\npaths: {}\n"},
		{name: "info without version", content: "openapi: 3.0.3\ninfo:\n  title: Orders\npaths: {}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := SniffSpec("openapi.yaml", []byte(tt.content)); ok {
				t.Error("SniffSpec() ok = true, want false")
			}
		})
	}
}

// "3 operations" for a service with thirty is worse than no number: it looks
// measured. An external `$ref` withdraws the count so the UI can render a dash.
func TestSniffSpec_ExternalRefWithdrawsTheOperationCount(t *testing.T) {
	content := `openapi: 3.0.3
info:
  title: Orders API
  version: 1.0.0
paths:
  /orders:
    $ref: './paths/orders.yaml'
`
	spec, ok := SniffSpec("openapi.yaml", []byte(content))
	if !ok {
		t.Fatal("SniffSpec() ok = false, want true")
	}
	if spec.OperationCount != nil {
		t.Errorf("operation_count = %v, want nil so the UI shows a dash", *spec.OperationCount)
	}
}

// An internal `$ref` points inside the same document and does not affect the
// operation count, so withdrawing the number there would lose a real one.
func TestSniffSpec_InternalRefKeepsTheOperationCount(t *testing.T) {
	content := `openapi: 3.0.3
info:
  title: Orders API
  version: 1.0.0
paths:
  /orders:
    get:
      responses:
        '200':
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Order'
components:
  schemas:
    Order: {}
`
	spec, ok := SniffSpec("openapi.yaml", []byte(content))
	if !ok {
		t.Fatal("SniffSpec() ok = false, want true")
	}
	if spec.OperationCount == nil || *spec.OperationCount != 1 {
		t.Errorf("operation_count = %v, want 1", spec.OperationCount)
	}
}

// ── protobuf and GraphQL: extension plus probe, and a lower tier ─────────────

// Neither format has a root marker, so the only available signal is the extension
// plus a probe for an entry point. That is a weaker claim than a declared
// `openapi:` field, and the rule id and confidence have to say so.
func TestSniffSpec_ProtoWithServiceIsGRPCAtTheLowerTier(t *testing.T) {
	content := `syntax = "proto3";
package orders.v1;

service OrderService {
  rpc Get(GetRequest) returns (Order);
}
`
	spec, ok := SniffSpec("proto/orders.proto", []byte(content))
	if !ok {
		t.Fatal("SniffSpec() ok = false, want true")
	}
	if spec.Kind != SpecGRPC {
		t.Errorf("kind = %q, want %q", spec.Kind, SpecGRPC)
	}
	if spec.RuleID != RuleAPIExtensionSniff {
		t.Errorf("rule_id = %q, want %q", spec.RuleID, RuleAPIExtensionSniff)
	}
	if spec.Confidence >= ConfidenceExactName {
		t.Errorf("confidence = %v, want a tier below an exact root-marker match", spec.Confidence)
	}
}

// A .proto holding only messages is a data schema, not an API.
func TestSniffSpec_ProtoWithoutServiceYieldsNoAPI(t *testing.T) {
	content := `syntax = "proto3";
package orders.v1;

message Order {
  string id = 1;
}
`
	if _, ok := SniffSpec("proto/orders.proto", []byte(content)); ok {
		t.Error("SniffSpec() ok = true, want false for a message-only proto")
	}
}

func TestSniffSpec_GraphQLRootTypes(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "type Query", content: "type Query {\n  orders: [Order!]!\n}\n", want: true},
		{name: "type Mutation", content: "type Mutation {\n  placeOrder(input: X): Order\n}\n", want: true},
		{name: "extend type Query", content: "extend type Query {\n  orders: [Order!]!\n}\n", want: true},
		// An SDL file with only object types is a fragment of a schema, not an
		// entry point — the same distinction as a referenced OpenAPI fragment.
		{name: "objects only", content: "type Order {\n  id: ID!\n}\n", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, ok := SniffSpec("schema.graphql", []byte(tt.content))
			if ok != tt.want {
				t.Fatalf("SniffSpec() ok = %v, want %v", ok, tt.want)
			}
			if ok && spec.Kind != SpecGraphQL {
				t.Errorf("kind = %q, want %q", spec.Kind, SpecGraphQL)
			}
		})
	}
}

// Malformed YAML is not an API and must not panic — a repository with a broken
// spec would otherwise take the worker down.
func TestSniffSpec_MalformedYAMLYieldsNoAPI(t *testing.T) {
	if _, ok := SniffSpec("openapi.yaml", []byte("openapi: 3.0.3\n  info:\n bad: [indent")); ok {
		t.Error("SniffSpec() ok = true, want false for malformed yaml")
	}
	if _, ok := SniffSpec("openapi.json", []byte(`{"openapi":`)); ok {
		t.Error("SniffSpec() ok = true, want false for malformed json")
	}
}

// A committed bundle is not a spec, and the size guard lives in the caller — this
// only checks the sniff survives something large without misreading it.
func TestSniffSpec_LargeNonSpecYieldsNoAPI(t *testing.T) {
	if _, ok := SniffSpec("api.yaml", []byte(strings.Repeat("- item\n", 5000))); ok {
		t.Error("SniffSpec() ok = true, want false for a plain yaml list")
	}
}
