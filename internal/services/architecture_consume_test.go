package services

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/derive"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
)

func hostsFact(t *testing.T, repositoryID string, complete bool, payload derive.HostsFact) models.RepositoryFact {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"data":    payload,
		"outcome": derive.Outcome{Complete: complete},
	})
	if err != nil {
		t.Fatalf("marshal fact payload: %v", err)
	}
	return models.RepositoryFact{
		OrganizationID: testOrgID,
		RepositoryID:   repositoryID,
		FactKind:       models.FactKindHosts,
		Payload:        encoded,
		Complete:       complete,
	}
}

func consumedFrom(host, envVar, evidencePath string) derive.HostFinding {
	return derive.HostFinding{
		Host: host, EnvVar: envVar, EvidencePath: evidencePath,
		RuleID: derive.RuleConsumeK8sServiceHost, Confidence: derive.ConfidenceDeclaredHost,
	}
}

func deriveConsume(t *testing.T, facts ...models.RepositoryFact) DerivedSet {
	t.Helper()
	set, err := consumeDeriver{}.Derive(context.Background(), testOrgID, facts)
	if err != nil {
		t.Fatalf("Derive() error = %v, want nil", err)
	}
	return set
}

func onlyEdge(t *testing.T, set DerivedSet) DerivedEdge {
	t.Helper()
	if len(set.Edges) != 1 {
		t.Fatalf("edges = %+v, want exactly one", set.Edges)
	}
	return set.Edges[0]
}

// ── tier 2a: the strongest static signal ─────────────────────────────────────

// In-cluster DNS is a naming *contract*: the name exists because someone declared
// a Service with it. So this match is against a declaration, not against a guess
// about how teams name things — which is what separates 2a from the tier 3 that is
// deliberately absent.
func TestConsume_ServiceHostInEnvValueYieldsEdge(t *testing.T) {
	set := deriveConsume(t,
		hostsFact(t, "repo-checkout", true, derive.HostsFact{
			ConsumedHosts: []derive.HostFinding{
				consumedFrom("orders-api", "CHECKOUT_ORDERS_URL", "k8s/deployment.yaml"),
			},
		}),
		hostsFact(t, "repo-orders", true, derive.HostsFact{DeclaredServices: []string{"orders-api"}}),
	)

	edge := onlyEdge(t, set)
	if edge.SourceRepositoryID != "repo-checkout" || edge.TargetRepositoryID != "repo-orders" {
		t.Errorf("edge = %s → %s, want repo-checkout → repo-orders", edge.SourceRepositoryID, edge.TargetRepositoryID)
	}
	if edge.Kind != models.RepositoryRelationshipKindHTTP {
		t.Errorf("kind = %q, want http", edge.Kind)
	}
	// `config` is the enum value migration 033 added for exactly this family.
	if edge.Source != models.RepositoryRelationshipSourceConfig {
		t.Errorf("source = %q, want config", edge.Source)
	}
	if edge.Confidence != derive.ConfidenceDeclaredHost {
		t.Errorf("confidence = %v, want the declared-host tier", edge.Confidence)
	}
	if edge.Metadata["tier"] != "2a" {
		t.Errorf("tier = %v, want 2a", edge.Metadata["tier"])
	}
	// The drawer needs both of these to let a person judge the edge in two
	// seconds: "via CHECKOUT_ORDERS_URL in k8s/deployment.yaml".
	if edge.Metadata["env_var_name"] != "CHECKOUT_ORDERS_URL" {
		t.Errorf("env_var_name = %v, want the variable", edge.Metadata["env_var_name"])
	}
	if edge.Metadata["evidence_path"] != "k8s/deployment.yaml" {
		t.Errorf("evidence_path = %v, want the manifest", edge.Metadata["evidence_path"])
	}
}

// A fully-qualified in-cluster name has to reduce to the first label, which is the
// Service name someone actually declared.
func TestConsume_FullyQualifiedClusterNameMatchesTheServiceLabel(t *testing.T) {
	set := deriveConsume(t,
		hostsFact(t, "repo-checkout", true, derive.HostsFact{
			ConsumedHosts: []derive.HostFinding{
				consumedFrom("orders.prod.svc.cluster.local", "ORDERS_HOST", "k8s/deployment.yaml"),
			},
		}),
		hostsFact(t, "repo-orders", true, derive.HostsFact{DeclaredServices: []string{"orders"}}),
	)

	edge := onlyEdge(t, set)
	if edge.TargetRepositoryID != "repo-orders" {
		t.Errorf("target = %q, want repo-orders", edge.TargetRepositoryID)
	}
}

// A host nothing in the organization declares produces nothing. That is what makes
// third-party hosts harmless without a denylist of their own.
func TestConsume_UndeclaredHostYieldsNoEdge(t *testing.T) {
	set := deriveConsume(t,
		hostsFact(t, "repo-checkout", true, derive.HostsFact{
			ConsumedHosts: []derive.HostFinding{
				consumedFrom("api.stripe.com", "STRIPE_URL", ".env.example"),
			},
		}),
		hostsFact(t, "repo-orders", true, derive.HostsFact{DeclaredServices: []string{"orders-api"}}),
	)

	if len(set.Edges) != 0 {
		t.Errorf("edges = %+v, want none for a third-party host", set.Edges)
	}
}

// The same ambiguity rule as the package index in phase 1: two repositories
// declaring the same Service name produce no edge, because guessing would be a
// wrong edge at high confidence.
func TestConsume_HostDeclaredByTwoRepositoriesYieldsNoEdge(t *testing.T) {
	set := deriveConsume(t,
		hostsFact(t, "repo-checkout", true, derive.HostsFact{
			ConsumedHosts: []derive.HostFinding{consumedFrom("orders-api", "ORDERS_URL", "k8s/deployment.yaml")},
		}),
		hostsFact(t, "repo-orders", true, derive.HostsFact{DeclaredServices: []string{"orders-api"}}),
		hostsFact(t, "repo-orders-legacy", true, derive.HostsFact{DeclaredServices: []string{"orders-api"}}),
	)

	if len(set.Edges) != 0 {
		t.Errorf("edges = %+v, want none while the name is contested", set.Edges)
	}
}

// The provider repository shipping a client of itself in its own compose file is
// real, and it is not an edge.
func TestConsume_SelfReferenceYieldsNoEdge(t *testing.T) {
	set := deriveConsume(t, hostsFact(t, "repo-orders", true, derive.HostsFact{
		DeclaredServices: []string{"orders-api"},
		ConsumedHosts:    []derive.HostFinding{consumedFrom("orders-api", "SELF_URL", "docker-compose.yml")},
	}))

	if len(set.Edges) != 0 {
		t.Errorf("edges = %+v, want none for a self reference", set.Edges)
	}
}

// ── tier 2b: public hostnames, matched only against declarations ─────────────

func TestConsume_IngressHostMatchesAtALowerTier(t *testing.T) {
	set := deriveConsume(t,
		hostsFact(t, "repo-checkout", true, derive.HostsFact{
			ConsumedHosts: []derive.HostFinding{
				consumedFrom("orders.acme.example", "ORDERS_PUBLIC_URL", "k8s/deployment.yaml"),
			},
		}),
		hostsFact(t, "repo-orders", true, derive.HostsFact{DeclaredHosts: []string{"orders.acme.example"}}),
	)

	edge := onlyEdge(t, set)
	if edge.Metadata["tier"] != "2b" {
		t.Errorf("tier = %v, want 2b", edge.Metadata["tier"])
	}
	// A public hostname is a weaker claim than an in-cluster Service name.
	if edge.Confidence >= derive.ConfidenceDeclaredHost {
		t.Errorf("confidence = %v, want below the in-cluster tier", edge.Confidence)
	}
}

// ── tier 1: the compose alias ────────────────────────────────────────────────

// A compose service name is a local alias the repository chose, not a DNS
// contract, so it earns a lower tier even when it matches a real declaration.
func TestConsume_ComposeAliasMatchesAtTierOne(t *testing.T) {
	set := deriveConsume(t,
		hostsFact(t, "repo-checkout", true, derive.HostsFact{
			ConsumedHosts: []derive.HostFinding{{
				Host: "orders-api", EvidencePath: "docker-compose.yml",
				RuleID: derive.RuleConsumeComposeDependsOn, Confidence: derive.ConfidenceComposeTopology,
			}},
		}),
		hostsFact(t, "repo-orders", true, derive.HostsFact{DeclaredServices: []string{"orders-api"}}),
	)

	edge := onlyEdge(t, set)
	if edge.Metadata["tier"] != "1" {
		t.Errorf("tier = %v, want 1", edge.Metadata["tier"])
	}
	if edge.Confidence != derive.ConfidenceComposeTopology {
		t.Errorf("confidence = %v, want the compose-topology tier", edge.Confidence)
	}
}

// A compose alias is a container name on a private network, so an Ingress host it
// happened to equal would be a coincidence, not a reference.
func TestConsume_ComposeAliasIsNeverMatchedAgainstAPublicHost(t *testing.T) {
	set := deriveConsume(t,
		hostsFact(t, "repo-checkout", true, derive.HostsFact{
			ConsumedHosts: []derive.HostFinding{{
				Host: "orders.acme.example", EvidencePath: "docker-compose.yml",
				RuleID: derive.RuleConsumeComposeDependsOn, Confidence: derive.ConfidenceComposeTopology,
			}},
		}),
		hostsFact(t, "repo-orders", true, derive.HostsFact{DeclaredHosts: []string{"orders.acme.example"}}),
	)

	if len(set.Edges) != 0 {
		t.Errorf("edges = %+v, want none — a compose alias is not a public hostname", set.Edges)
	}
}

// ── agreement ────────────────────────────────────────────────────────────────

// Two tiers naming the same pair take the max and keep both pieces of evidence.
// Never sum and never multiply: rules reading the same file share a root cause, so
// combining them as independent signals inflates the number.
func TestConsume_AgreementTakesMaxNotProduct(t *testing.T) {
	set := deriveConsume(t,
		hostsFact(t, "repo-checkout", true, derive.HostsFact{
			ConsumedHosts: []derive.HostFinding{
				// tier 1 via the compose alias...
				{Host: "orders-api", EvidencePath: "docker-compose.yml",
					RuleID: derive.RuleConsumeComposeDependsOn, Confidence: derive.ConfidenceComposeTopology},
				// ...and tier 2a via the manifest's env value.
				consumedFrom("orders-api", "ORDERS_URL", "k8s/deployment.yaml"),
			},
		}),
		hostsFact(t, "repo-orders", true, derive.HostsFact{DeclaredServices: []string{"orders-api"}}),
	)

	edge := onlyEdge(t, set)
	if edge.Confidence != derive.ConfidenceDeclaredHost {
		t.Errorf("confidence = %v, want the max of the two tiers (%v)", edge.Confidence, derive.ConfidenceDeclaredHost)
	}
	if edge.Confidence > 1 {
		t.Errorf("confidence = %v, want agreement never to exceed 1", edge.Confidence)
	}
	if edge.Metadata["tier"] != "2a" {
		t.Errorf("tier = %v, want the stronger rule to own the row", edge.Metadata["tier"])
	}
	// Both files that produced the edge are recorded, because a person judging it
	// wants to see every one of them.
	also, _ := edge.Metadata["also_evidence"].([]string)
	if len(also) == 0 {
		t.Errorf("also_evidence = %v, want the second evidence path kept", edge.Metadata["also_evidence"])
	}
}

// ── the limitation with no fix ───────────────────────────────────────────────

// A repository that mocks B in its tests is statically indistinguishable from one
// that calls B. The edge is still recorded — the drawer's job is to let a person
// judge it — but the rule id says which file won, so "this is a mock" is visible
// rather than hidden.
func TestConsume_PrefersProductionComposeOverTestCompose(t *testing.T) {
	testOnly := deriveConsume(t,
		hostsFact(t, "repo-checkout", true, derive.HostsFact{
			ConsumedHosts: []derive.HostFinding{{
				Host: "orders-api", EnvVar: "ORDERS_URL", EvidencePath: "docker-compose.test.yml",
				RuleID: derive.RuleConsumeK8sServiceHost, Confidence: derive.ConfidenceDeclaredHost,
				FromTestFile: true,
			}},
		}),
		hostsFact(t, "repo-orders", true, derive.HostsFact{DeclaredServices: []string{"orders-api"}}),
	)
	edge := onlyEdge(t, testOnly)
	ruleID, _ := edge.Metadata["rule_id"].(string)
	if ruleID != derive.RuleConsumeK8sServiceHost+".test_file" {
		t.Errorf("rule_id = %q, want the test-file variant so a mock is visible", ruleID)
	}
	if fromTest, _ := edge.Metadata["from_test_file"].(bool); !fromTest {
		t.Error("from_test_file = false, want true")
	}

	// When both a production and a test file name the host, the production one is
	// what the row reports.
	both := deriveConsume(t,
		hostsFact(t, "repo-checkout", true, derive.HostsFact{
			ConsumedHosts: []derive.HostFinding{
				{Host: "orders-api", EnvVar: "ORDERS_URL", EvidencePath: "docker-compose.test.yml",
					RuleID: derive.RuleConsumeK8sServiceHost, Confidence: derive.ConfidenceDeclaredHost, FromTestFile: true},
				{Host: "orders-api", EnvVar: "ORDERS_URL", EvidencePath: "k8s/deployment.yaml",
					RuleID: derive.RuleConsumeK8sServiceHost, Confidence: derive.ConfidenceDeclaredHost},
			},
		}),
		hostsFact(t, "repo-orders", true, derive.HostsFact{DeclaredServices: []string{"orders-api"}}),
	)
	winner := onlyEdge(t, both)
	winnerRule, _ := winner.Metadata["rule_id"].(string)
	if winnerRule != derive.RuleConsumeK8sServiceHost {
		t.Errorf("rule_id = %q, want the production rule to win", winnerRule)
	}
}

// ── broker edges ─────────────────────────────────────────────────────────────

// The variable name is consulted in exactly one place, and it decides only the
// edge's kind — never whether the edge exists. Getting it wrong mislabels an edge a
// declaration already proved; it cannot invent one.
func TestConsume_BrokerVariableYieldsAnAsyncEdge(t *testing.T) {
	set := deriveConsume(t,
		hostsFact(t, "repo-checkout", true, derive.HostsFact{
			ConsumedHosts: []derive.HostFinding{
				consumedFrom("orders-events", "ORDERS_KAFKA_BROKER", "k8s/deployment.yaml"),
			},
		}),
		hostsFact(t, "repo-orders", true, derive.HostsFact{DeclaredServices: []string{"orders-events"}}),
	)

	edge := onlyEdge(t, set)
	if edge.Kind != models.RepositoryRelationshipKindAsync {
		t.Errorf("kind = %q, want async", edge.Kind)
	}
}

// ── the completeness gate ────────────────────────────────────────────────────

// The index is org-wide, so one repository's incomplete extraction withdraws the
// sweep for everyone: a Service name it failed to declare hides an edge from every
// other repository to it.
func TestConsume_IncompleteFactWithdrawsTheSweep(t *testing.T) {
	set := deriveConsume(t,
		hostsFact(t, "repo-checkout", true, derive.HostsFact{
			ConsumedHosts: []derive.HostFinding{consumedFrom("orders-api", "ORDERS_URL", "k8s/deployment.yaml")},
		}),
		hostsFact(t, "repo-orders", true, derive.HostsFact{DeclaredServices: []string{"orders-api"}}),
		hostsFact(t, "repo-truncated", false, derive.HostsFact{}),
	)

	if set.Complete {
		t.Error("Complete = true, want false — one repository could not be fully inspected")
	}
	// The edge it could see is still asserted; only retraction is withheld.
	if len(set.Edges) != 1 {
		t.Errorf("edges = %+v, want the observable edge still written", set.Edges)
	}
}

func TestConsume_KeyCarriesTheDeriverVersionAndOrganizationScope(t *testing.T) {
	if got := (consumeDeriver{}).Key("org-42"); got != "apiconsume:v1:org/org-42" {
		t.Errorf("Key() = %q, want %q", got, "apiconsume:v1:org/org-42")
	}
}

// ── tier 3 is deliberately absent ────────────────────────────────────────────

// The tempting tier, and the one that generates the noise: `ORDERS_API_URL` →
// repository `orders`. A variable's *name* reflects what a team calls a service,
// not a declaration — it may point at a third party, a mock, a gateway, or nothing.
//
// This asserts the absence directly: a variable whose name screams "orders", whose
// value matches nothing anyone declared, produces no edge. If tier 3 is ever added
// it must be off by default with confidence <= 0.40 and a badge on the edge, and
// this test is what will fail first to say so.
func TestConsume_VariableNameAloneNeverYieldsEdge(t *testing.T) {
	set := deriveConsume(t,
		hostsFact(t, "repo-checkout", true, derive.HostsFact{
			ConsumedHosts: []derive.HostFinding{
				consumedFrom("orders-gateway.thirdparty.example", "ORDERS_API_URL", "k8s/deployment.yaml"),
			},
		}),
		// The repository is called orders and declares a Service — but not the one
		// the value names.
		hostsFact(t, "repo-orders", true, derive.HostsFact{
			DeclaredServices: []string{"orders-api"},
			DeclaredHosts:    []string{"orders.acme.example"},
		}),
	)

	if len(set.Edges) != 0 {
		t.Errorf("edges = %+v, want none — the variable name is not evidence", set.Edges)
	}
}
