package models

import (
	"time"

	"gorm.io/datatypes"
)

// Graph node kinds. The discriminator on every node, so a client knows what it
// received and where a click should route.
const (
	GraphNodeKindRepo     = "repo"
	GraphNodeKindAPI      = "api"
	GraphNodeKindResource = "resource"
)

// GraphNodeID renders a prefixed node id.
//
// Three tables sharing one id namespace invites collision, and a bare UUID tells
// the frontend nothing about what it is holding. Prefixing solves both at once.
// The alternative — bare UUIDs plus a `kind` field — works, but then every
// consumer has to carry the pair around and an edge endpoint has no way to say
// which table it points into.
func GraphNodeID(kind, id string) string {
	switch kind {
	case GraphNodeKindAPI:
		return "api:" + id
	case GraphNodeKindResource:
		return "resource:" + id
	default:
		return "repo:" + id
	}
}

type RepositoryGraphResponse struct {
	Nodes []RepositoryGraphNode `json:"nodes"`
	Edges []RepositoryGraphEdge `json:"edges"`
}

// RepositoryGraphNode is one node of the typed graph.
//
// It is a union flattened into one struct rather than three, because the wire
// format is JSON and a discriminated union there is a `kind` plus optional
// fields. The kind-specific groups below are only ever populated for their own
// kind.
//
// Every field the Go side always sends is required on the TypeScript side, and
// every `omitempty` field is optional there. Crossing those wires is the bug
// FOLLOWUPS.md records twice — a numeric field with `omitempty` against a
// required schema renders as a silent empty state.
type RepositoryGraphNode struct {
	// ID is prefixed: `repo:<uuid>`, `api:<uuid>`, `resource:<uuid>`.
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`

	// ── repo ─────────────────────────────────────────────────────────────────

	Description string              `json:"description,omitempty"`
	URL         string              `json:"url,omitempty"`
	Type        RepositoryType      `json:"type,omitempty"`
	Metadata    *RepositoryMetadata `json:"metadata,omitempty"`
	SyncStatus  string              `json:"sync_status,omitempty"`

	// ── api ──────────────────────────────────────────────────────────────────

	Title    string `json:"title,omitempty"`
	Version  string `json:"version,omitempty"`
	SpecKind string `json:"spec_kind,omitempty"`
	SpecPath string `json:"spec_path,omitempty"`
	// OperationCount stays a pointer through the whole stack: nil means `$ref`
	// made the count unreliable, and the UI must render a dash rather than a
	// confident zero.
	OperationCount *int `json:"operation_count,omitempty"`

	// ── resource ─────────────────────────────────────────────────────────────

	Engine    string `json:"engine,omitempty"`
	Host      string `json:"host,omitempty"`
	Port      *int   `json:"port,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	// IsScoped is true when the resource belongs to a single repository because
	// the evidence identified an engine and not an instance. A pointer so a
	// resource node always carries it: "local to this repository" is the honest
	// label, and omitting it would let the UI imply a shared database that does
	// not exist.
	IsScoped *bool `json:"is_scoped,omitempty"`

	// RepositoryID is the owning repository of an api or resource node, so the
	// drawer can link back without another request.
	RepositoryID string `json:"repository_id,omitempty"`
	// RuleID and Evidence explain a derived node the way they explain a derived
	// edge.
	RuleID   string   `json:"rule_id,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
}

// RepositoryGraphEdge is one edge of the typed graph.
//
// Source and Target are generic prefixed node ids rather than
// `source_repository_id`/`target_repository_id`, because an edge can now end on
// an api or a resource. Keeping the old fields alongside the new ones was
// considered and rejected: it doubles the contract to save one small refactor and
// leaves two sets of fields that must agree forever.
//
// The provenance enum moves to `provenance` because `source` now means an
// endpoint. Renaming it is the honest option — leaving `source` meaning two
// different things depending on which struct you are holding is how a contract
// becomes a trap.
type RepositoryGraphEdge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`

	Kind        RepositoryRelationshipKind   `json:"kind"`
	Label       string                       `json:"label,omitempty"`
	Description string                       `json:"description,omitempty"`
	Provenance  RepositoryRelationshipSource `json:"provenance"`
	Confidence  float64                      `json:"confidence"`
	// DerivationKey is empty for an edge a person declared and non-empty for a
	// derived one. It is on the graph payload because the render depends on it:
	// a derived edge must never look like a declaration, and the cut between the
	// two visual buckets is exactly "is this field empty".
	DerivationKey string            `json:"derivation_key,omitempty"`
	Metadata      datatypes.JSONMap `json:"metadata,omitempty"`
}

type CreateRepositoryRelationshipRequest struct {
	SourceRepositoryID string                     `json:"source_repository_id" binding:"required"`
	TargetRepositoryID string                     `json:"target_repository_id" binding:"required"`
	Kind               RepositoryRelationshipKind `json:"kind" binding:"required"`
	Label              string                     `json:"label,omitempty"`
	Description        string                     `json:"description,omitempty"`
	Metadata           datatypes.JSONMap          `json:"metadata,omitempty"`
}

type UpdateRepositoryRelationshipRequest struct {
	Kind        *RepositoryRelationshipKind `json:"kind,omitempty"`
	Label       *string                     `json:"label,omitempty"`
	Description *string                     `json:"description,omitempty"`
	Confidence  *float64                    `json:"confidence,omitempty"`
	Metadata    datatypes.JSONMap           `json:"metadata,omitempty"`
}

type RepositoryRelationshipResponse struct {
	ID                 string                       `json:"id"`
	OrganizationID     string                       `json:"organization_id"`
	SourceRepositoryID string                       `json:"source_repository_id"`
	TargetRepositoryID string                       `json:"target_repository_id"`
	Kind               RepositoryRelationshipKind   `json:"kind"`
	Label              string                       `json:"label,omitempty"`
	Description        string                       `json:"description,omitempty"`
	Source             RepositoryRelationshipSource `json:"source"`
	Confidence         float64                      `json:"confidence"`
	Metadata           datatypes.JSONMap            `json:"metadata,omitempty"`
	CreatedByUserID    string                       `json:"created_by_user_id,omitempty"`
	CreatedAt          time.Time                    `json:"created_at"`
	UpdatedAt          time.Time                    `json:"updated_at"`
}

func RepositoryRelationshipToResponse(r *RepositoryRelationship) *RepositoryRelationshipResponse {
	return &RepositoryRelationshipResponse{
		ID:                 r.ID,
		OrganizationID:     r.OrganizationID,
		SourceRepositoryID: r.SourceRepositoryID,
		TargetRepositoryID: r.TargetRepositoryID,
		Kind:               r.Kind,
		Label:              r.Label,
		Description:        r.Description,
		Source:             r.Source,
		Confidence:         r.Confidence,
		Metadata:           r.Metadata,
		CreatedByUserID:    r.CreatedByUserID,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
	}
}

func RepositoryToGraphNode(r *Repository) RepositoryGraphNode {
	metadata := r.Metadata
	return RepositoryGraphNode{
		ID:          GraphNodeID(GraphNodeKindRepo, r.ID),
		Kind:        GraphNodeKindRepo,
		Name:        r.Name,
		Description: r.Description,
		URL:         r.URL,
		Type:        r.Type,
		Metadata:    &metadata,
		SyncStatus:  r.SyncStatus,
	}
}

// APIToGraphNode renders a discovered API as a node.
//
// Title is preferred over the spec path for the label because it is what a person
// recognizes; the path is still carried, since it is the identity and the thing the
// drawer links to on the provider.
func APIToGraphNode(a *API) RepositoryGraphNode {
	name := a.Title
	if name == "" {
		name = a.SpecPath
	}
	node := RepositoryGraphNode{
		ID:             GraphNodeID(GraphNodeKindAPI, a.ID),
		Kind:           GraphNodeKindAPI,
		Name:           name,
		Title:          a.Title,
		Version:        a.Version,
		SpecKind:       string(a.Kind),
		SpecPath:       a.SpecPath,
		OperationCount: a.OperationCount,
		RepositoryID:   a.RepositoryID,
	}
	if a.Metadata != nil {
		if ruleID, ok := a.Metadata["rule_id"].(string); ok {
			node.RuleID = ruleID
		}
	}
	return node
}

// ResourceToGraphNode renders a resource as a node.
//
// IsScoped is always populated, never left nil, because the difference between "a
// database three services share" and "a database in this repository's compose
// file" is the single most important thing this node has to communicate.
func ResourceToGraphNode(r *Resource) RepositoryGraphNode {
	name := r.DisplayName
	if name == "" {
		name = r.Engine
	}
	scoped := r.IsScoped()
	node := RepositoryGraphNode{
		ID:        GraphNodeID(GraphNodeKindResource, r.ID),
		Kind:      GraphNodeKindResource,
		Name:      name,
		Engine:    r.Engine,
		Host:      r.Host,
		Port:      r.Port,
		Namespace: r.Namespace,
		IsScoped:  &scoped,
	}
	if r.ScopedRepositoryID != nil {
		node.RepositoryID = *r.ScopedRepositoryID
	}
	if r.Metadata != nil {
		if ruleID, ok := r.Metadata["rule_id"].(string); ok {
			node.RuleID = ruleID
		}
		node.Evidence = stringsFromJSONMap(r.Metadata["evidence"])
	}
	return node
}

// stringsFromJSONMap reads a JSONB array of strings back out.
//
// It has to handle []any as well as []string: a value that made a round trip
// through JSONB comes back as []any, and a value set in the same process is still
// []string. Handling only one of them is how a field silently reads as empty after
// a restart.
func stringsFromJSONMap(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func RepositoryRelationshipToGraphEdge(r *RepositoryRelationship, includeMetadata bool) RepositoryGraphEdge {
	edge := RepositoryGraphEdge{
		ID:          r.ID,
		Source:      GraphNodeID(GraphNodeKindRepo, r.SourceRepositoryID),
		Target:      GraphNodeID(GraphNodeKindRepo, r.TargetRepositoryID),
		Kind:        r.Kind,
		Label:       r.Label,
		Description: r.Description,
		Provenance:  r.Source,
		Confidence:  r.Confidence,
	}
	if r.DerivationKey != nil {
		edge.DerivationKey = *r.DerivationKey
	}
	if includeMetadata {
		edge.Metadata = r.Metadata
	}
	return edge
}

// APIProvidesEdge is the repo → api edge.
//
// It is synthesized rather than stored: the API row already names its repository,
// so a `repository_relationships` row saying the same thing would be a second copy
// of one fact that could disagree with the first.
func APIProvidesEdge(a *API) RepositoryGraphEdge {
	return RepositoryGraphEdge{
		ID:         "provides:" + a.ID,
		Source:     GraphNodeID(GraphNodeKindRepo, a.RepositoryID),
		Target:     GraphNodeID(GraphNodeKindAPI, a.ID),
		Kind:       RepositoryRelationshipKindProvides,
		Provenance: RepositoryRelationshipSourceManifest,
		Confidence: 1.0,
	}
}

// ResourceUsesEdge is the repo → resource edge, built from the join row.
//
// Unlike `provides` this one carries the join's confidence, because the strength of
// the claim varies: a shared locator is stronger evidence than a compose image.
func ResourceUsesEdge(link *RepositoryResource, includeMetadata bool) RepositoryGraphEdge {
	edge := RepositoryGraphEdge{
		ID:         link.ID,
		Source:     GraphNodeID(GraphNodeKindRepo, link.RepositoryID),
		Target:     GraphNodeID(GraphNodeKindResource, link.ResourceID),
		Kind:       RepositoryRelationshipKindUses,
		Provenance: RepositoryRelationshipSourceConfig,
		Confidence: link.Confidence,
	}
	if link.DerivationKey != nil {
		edge.DerivationKey = *link.DerivationKey
	}
	if includeMetadata {
		edge.Metadata = link.Metadata
	}
	return edge
}
