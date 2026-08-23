//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/testsupport/fakegitlab"
)

const (
	checkoutURL = "https://gitlab.com/" + fakegitlab.CheckoutPath
	sharedURL   = "https://gitlab.com/" + fakegitlab.SharedPath
)

type graphNode struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	// api
	SpecKind       string `json:"spec_kind"`
	SpecPath       string `json:"spec_path"`
	Version        string `json:"version"`
	OperationCount *int   `json:"operation_count"`
	// resource
	Engine   string `json:"engine"`
	IsScoped *bool  `json:"is_scoped"`
	Host     string `json:"host"`
	// both
	RepositoryID string `json:"repository_id"`
	RuleID       string `json:"rule_id"`
}

type graphEdge struct {
	ID            string         `json:"id"`
	Source        string         `json:"source"`
	Target        string         `json:"target"`
	Kind          string         `json:"kind"`
	Label         string         `json:"label"`
	Provenance    string         `json:"provenance"`
	Confidence    float64        `json:"confidence"`
	DerivationKey string         `json:"derivation_key"`
	Metadata      map[string]any `json:"metadata"`
}

type graphResponse struct {
	Nodes []graphNode `json:"nodes"`
	Edges []graphEdge `json:"edges"`
}

func (g graphResponse) nodeByID(id string) (graphNode, bool) {
	for _, node := range g.Nodes {
		if node.ID == id {
			return node, true
		}
	}
	return graphNode{}, false
}

func (g graphResponse) nodesOfKind(kind string) []graphNode {
	var out []graphNode
	for _, node := range g.Nodes {
		if node.Kind == kind {
			out = append(out, node)
		}
	}
	return out
}

func (g graphResponse) edgesOfKind(kind string) []graphEdge {
	var out []graphEdge
	for _, edge := range g.Edges {
		if edge.Kind == kind {
			out = append(out, edge)
		}
	}
	return out
}

// graph asks for every node kind, because the server's default leaves resources
// off and this suite is asserting that all three arrive.
func (s *stack) graph(t *testing.T, token string) graphResponse {
	t.Helper()
	resp := s.mustDo(t, http.MethodGet,
		"/api/v1/repositories/graph?include_metadata=true&node_kinds=repo,api,resource",
		token, nil, http.StatusOK)
	var graph graphResponse
	resp.decode(t, &graph)
	return graph
}

// waitForDerivedEdge polls the graph until an edge between the two repositories
// shows up.
//
// Polling is necessary rather than lazy: reconciliation is a debounced background
// task, so the edge appears strictly after both syncs have finished.
func (s *stack) waitForDerivedEdge(t *testing.T, token, sourceID, targetID string) graphResponse {
	t.Helper()
	var graph graphResponse
	err := waitFor(90*time.Second, func() error {
		graph = s.graph(t, token)
		for _, edge := range graph.Edges {
			if edge.Source == sourceID && edge.Target == targetID {
				return nil
			}
		}
		return fmt.Errorf("no edge %s → %s yet (%d edges)", sourceID, targetID, len(graph.Edges))
	})
	if err != nil {
		t.Fatalf("derived edge never appeared: %v", err)
	}
	return graph
}

// TestArchitectureDerivation is the claim of the whole domain, tested end to end:
// nobody configures anything, and the graph fills itself in.
//
// It also proves the guarantee that makes derivation safe to run on every sync —
// a human-declared edge survives re-derivation untouched. That one is asserted
// against real Postgres here, because the partial unique index and the
// key-scoped sweep are SQL, and unit tests can only model them.
func TestArchitectureDerivation(t *testing.T) {
	token := sut.registerOrg(t, "architecture")
	configureGitLabToken(t, token)

	checkout := sut.createRepository(t, token, checkoutURL)
	shared := sut.createRepository(t, token, sharedURL)

	for _, repo := range []struct {
		name string
		id   string
	}{{"checkout", checkout.ID}, {"shared", shared.ID}} {
		synced := sut.waitForSync(t, token, repo.id)
		if synced.SyncStatus != "synced" {
			t.Fatalf("%s sync_status = %q (error: %q), want synced", repo.name, synced.SyncStatus, synced.SyncError)
		}
	}

	checkoutNode := "repo:" + checkout.ID
	sharedNode := "repo:" + shared.ID
	graph := sut.waitForDerivedEdge(t, token, checkoutNode, sharedNode)

	var derived graphEdge
	for _, edge := range graph.Edges {
		if edge.Source == checkoutNode && edge.Target == sharedNode && edge.Kind == "library" {
			derived = edge
		}
	}

	t.Run("the edge is a library dependency read from the manifest", func(t *testing.T) {
		if derived.Kind != "library" {
			t.Errorf("kind = %q, want library", derived.Kind)
		}
		// `manifest` was in migration 012's CHECK from the start and unused
		// until now.
		if derived.Provenance != "manifest" {
			t.Errorf("provenance = %q, want manifest", derived.Provenance)
		}
		// An exact name match against the organization's own index. Nothing was
		// guessed, so nothing less than 1.00 would be honest.
		if derived.Confidence != 1 {
			t.Errorf("confidence = %v, want 1", derived.Confidence)
		}
		if derived.Label != "gitlab.com/e2e-org/shared" {
			t.Errorf("label = %q, want the module path", derived.Label)
		}
		if derived.DerivationKey == "" {
			t.Error("derivation_key is empty, want the libdep key that scopes its sweep")
		}
	})

	t.Run("the drawer has the evidence a person needs to judge the edge", func(t *testing.T) {
		if derived.Metadata["rule_id"] != "libdep.gomod" {
			t.Errorf("rule_id = %v, want libdep.gomod", derived.Metadata["rule_id"])
		}
		if derived.Metadata["manifest_path"] != "go.mod" {
			t.Errorf("manifest_path = %v, want go.mod", derived.Metadata["manifest_path"])
		}
		if derived.Metadata["declared_version"] != "v1.2.0" {
			t.Errorf("declared_version = %v, want v1.2.0", derived.Metadata["declared_version"])
		}
	})

	// The three node kinds, in one payload. This is the claim of phase 5: the graph
	// stopped being repo→repo and became architecture.
	t.Run("the graph carries all three node kinds", func(t *testing.T) {
		typed := sut.graph(t, token)

		repos := typed.nodesOfKind("repo")
		if len(repos) < 2 {
			t.Fatalf("repo nodes = %d, want at least 2", len(repos))
		}
		for _, node := range typed.Nodes {
			// Every id is prefixed so three tables can share one namespace and a
			// click knows where to route.
			if !strings.HasPrefix(node.ID, node.Kind+":") {
				t.Errorf("node id = %q, want it prefixed with %q", node.ID, node.Kind+":")
			}
		}

		apis := typed.nodesOfKind("api")
		if len(apis) != 1 {
			t.Fatalf("api nodes = %+v, want exactly the checkout spec", apis)
		}
		api := apis[0]
		if api.SpecKind != "openapi" || api.SpecPath != "openapi.yaml" {
			t.Errorf("api node = %+v, want the openapi at openapi.yaml", api)
		}
		if api.Version != "1.4.0" {
			t.Errorf("api version = %q, want 1.4.0", api.Version)
		}
		// The spec declares two operations inline and no external $ref, so the
		// count is trustworthy and has to be reported.
		if api.OperationCount == nil || *api.OperationCount != 2 {
			t.Errorf("operation_count = %v, want 2", api.OperationCount)
		}
		if api.RepositoryID != checkout.ID {
			t.Errorf("api repository_id = %q, want the checkout repository", api.RepositoryID)
		}

		resources := typed.nodesOfKind("resource")
		if len(resources) != 1 {
			t.Fatalf("resource nodes = %+v, want the compose postgres", resources)
		}
		resource := resources[0]
		if resource.Engine != "postgresql" {
			t.Errorf("engine = %q, want postgresql", resource.Engine)
		}
		// A compose image proves the engine and nothing about which instance, so
		// the resource must come out scoped to its repository rather than implying
		// a shared database.
		if resource.IsScoped == nil || !*resource.IsScoped {
			t.Errorf("is_scoped = %v, want true for compose-only evidence", resource.IsScoped)
		}
		if resource.Host != "" {
			t.Errorf("host = %q, want empty — compose names no instance", resource.Host)
		}

		// `provides` points repo → api and `uses` points repo → resource, both with
		// generic prefixed endpoints.
		provides := typed.edgesOfKind("provides")
		if len(provides) != 1 || provides[0].Source != checkoutNode || provides[0].Target != api.ID {
			t.Errorf("provides edges = %+v, want %s → %s", provides, checkoutNode, api.ID)
		}
		uses := typed.edgesOfKind("uses")
		if len(uses) != 1 || uses[0].Source != checkoutNode || uses[0].Target != resource.ID {
			t.Errorf("uses edges = %+v, want %s → %s", uses, checkoutNode, resource.ID)
		}
	})

	// Tier 2a end to end: checkout's manifest names a host, shared declares a
	// Kubernetes Service with that name, and the match is against a declaration
	// rather than a guess about naming.
	t.Run("a consumption edge is derived from a declared service name", func(t *testing.T) {
		var consume graphEdge
		err := waitFor(90*time.Second, func() error {
			typed := sut.graph(t, token)
			for _, edge := range typed.Edges {
				if edge.Source == checkoutNode && edge.Target == sharedNode && edge.Kind == "http" &&
					edge.DerivationKey != "" {
					consume = edge
					return nil
				}
			}
			return fmt.Errorf("no derived http edge %s → %s yet", checkoutNode, sharedNode)
		})
		if err != nil {
			t.Fatalf("consumption edge never appeared: %v", err)
		}

		// `config` is the enum value migration 033 added for this family.
		if consume.Provenance != "config" {
			t.Errorf("provenance = %q, want config", consume.Provenance)
		}
		if consume.Confidence != 0.85 {
			t.Errorf("confidence = %v, want the declared-host tier", consume.Confidence)
		}
		if consume.Metadata["rule_id"] != "consume.k8s_service_host" {
			t.Errorf("rule_id = %v, want consume.k8s_service_host", consume.Metadata["rule_id"])
		}
		// The drawer needs both to let a person judge the edge — which for a
		// consumption edge is the only defence against a repository that mocks its
		// dependency in tests.
		if consume.Metadata["evidence_path"] != "k8s/deployment.yaml" {
			t.Errorf("evidence_path = %v, want the manifest", consume.Metadata["evidence_path"])
		}
		if consume.Metadata["env_var_name"] != "SHARED_API_URL" {
			t.Errorf("env_var_name = %v, want SHARED_API_URL", consume.Metadata["env_var_name"])
		}
	})

	// The threshold is what lets a person see "only what is certain" in one click,
	// and it has to cut across all three edge sources.
	t.Run("the confidence threshold hides the weaker edges", func(t *testing.T) {
		resp := sut.mustDo(t, http.MethodGet,
			"/api/v1/repositories/graph?include_metadata=true&node_kinds=repo,api,resource&min_confidence=0.95",
			token, nil, http.StatusOK)
		var filtered graphResponse
		resp.decode(t, &filtered)

		for _, edge := range filtered.Edges {
			if edge.Confidence < 0.95 {
				t.Errorf("edge %s has confidence %v, want it hidden above the threshold", edge.ID, edge.Confidence)
			}
		}
		// The library edge is 1.00, so it survives; the 0.85 consumption edge and
		// the 0.70 resource claim must not.
		if len(filtered.edgesOfKind("library")) == 0 {
			t.Error("the 1.00 library edge was hidden, want it kept")
		}
		if len(filtered.edgesOfKind("uses")) != 0 {
			t.Error("a 0.70 uses edge survived a 0.95 threshold")
		}
	})

	// A node kind that is off must not be fetched at all, and its edges must go
	// with it — an edge to a filtered node draws a line into nowhere.
	t.Run("turning a node kind off drops its dangling edges", func(t *testing.T) {
		resp := sut.mustDo(t, http.MethodGet,
			"/api/v1/repositories/graph?include_metadata=true&node_kinds=repo",
			token, nil, http.StatusOK)
		var reposOnly graphResponse
		resp.decode(t, &reposOnly)

		if len(reposOnly.nodesOfKind("api")) != 0 || len(reposOnly.nodesOfKind("resource")) != 0 {
			t.Errorf("nodes = %+v, want repositories only", reposOnly.Nodes)
		}
		if len(reposOnly.edgesOfKind("provides")) != 0 || len(reposOnly.edgesOfKind("uses")) != 0 {
			t.Errorf("edges = %+v, want the provides/uses edges dropped with their nodes", reposOnly.Edges)
		}
		// The repo→repo edges are untouched.
		if len(reposOnly.edgesOfKind("library")) == 0 {
			t.Error("the library edge disappeared, want repo→repo edges kept")
		}
	})

	// The structural guarantee, against real SQL. A human row carries a NULL
	// derivation key, and the sweep's `derivation_key = $1` cannot match NULL —
	// so no amount of re-derivation can retract a declaration. The partial
	// unique index is what keeps the derived row from colliding with it.
	t.Run("a human-declared edge survives re-derivation", func(t *testing.T) {
		resp := sut.mustDo(t, http.MethodPost, "/api/v1/repository-relationships", token, map[string]any{
			"source_repository_id": checkout.ID,
			"target_repository_id": shared.ID,
			"kind":                 "http",
			"label":                "checkout calls shared over HTTP",
		}, http.StatusCreated)
		var manual struct {
			ID string `json:"id"`
		}
		resp.decode(t, &manual)

		// Re-sync both repositories, which re-runs extraction and queues a
		// fresh reconciliation over the same facts.
		for _, id := range []string{checkout.ID, shared.ID} {
			sut.mustDo(t, http.MethodPost, "/api/v1/repositories/"+id+"/sync", token, nil, http.StatusAccepted)
		}
		for _, id := range []string{checkout.ID, shared.ID} {
			if synced := sut.waitForSync(t, token, id); synced.SyncStatus != "synced" {
				t.Fatalf("re-sync of %s = %q, want synced", id, synced.SyncStatus)
			}
		}

		// Give the debounced reconciliation room to run and then sweep.
		after := sut.waitForDerivedEdge(t, token, checkoutNode, sharedNode)

		manualFound, derivedCount := false, 0
		for _, edge := range after.Edges {
			if edge.ID == manual.ID {
				manualFound = true
				if edge.DerivationKey != "" {
					t.Errorf("the manual edge grew a derivation_key %q, want none", edge.DerivationKey)
				}
				if edge.Provenance != "manual" {
					t.Errorf("manual edge provenance = %q, want manual", edge.Provenance)
				}
			}
			if edge.Source == checkoutNode && edge.Target == sharedNode && edge.Kind == "library" {
				derivedCount++
			}
		}
		if !manualFound {
			t.Errorf("the human-declared edge is gone after re-derivation (edges: %+v)", after.Edges)
		}
		// Re-deriving must not duplicate: the partial unique index plus the
		// revive-instead-of-insert upsert are what keep this at one.
		if derivedCount != 1 {
			t.Errorf("derived library edges = %d, want exactly 1 after re-derivation", derivedCount)
		}
	})
}
