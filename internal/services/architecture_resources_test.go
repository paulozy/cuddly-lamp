package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/derive"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
)

const repoCompose = `services:
  api:
    build: .
    depends_on:
      - db
    environment:
      DATABASE_URL: postgres://svc:secret@db:5432/app
  db:
    image: postgres:16
`

// ── the extractor ────────────────────────────────────────────────────────────

func configInput(contents map[string]string) ExtractInput {
	paths := make([]string, 0, len(contents))
	for path := range contents {
		paths = append(paths, path)
	}
	reader := &stubFileReader{contents: contents}
	return ExtractInput{
		RepositoryID: "repo-orders",
		Paths:        paths,
		Fetch: func(ctx context.Context, path string) ([]byte, error) {
			return reader.GetFileContent(ctx, scm.RepoRef{}, "main", path)
		},
	}
}

func extractResources(t *testing.T, in ExtractInput) (derive.ResourcesFact, derive.Outcome) {
	t.Helper()
	payload, outcome := resourcesExtractor{}.Extract(context.Background(), in)
	fact, ok := payload.(derive.ResourcesFact)
	if !ok {
		t.Fatalf("payload type = %T, want derive.ResourcesFact", payload)
	}
	return fact, outcome
}

func extractHosts(t *testing.T, in ExtractInput) (derive.HostsFact, derive.Outcome) {
	t.Helper()
	payload, outcome := hostsExtractor{}.Extract(context.Background(), in)
	fact, ok := payload.(derive.HostsFact)
	if !ok {
		t.Fatalf("payload type = %T, want derive.HostsFact", payload)
	}
	return fact, outcome
}

func TestResourcesExtractor_ReadsComposeHelmAndManifests(t *testing.T) {
	fact, outcome := extractResources(t, configInput(map[string]string{
		"docker-compose.yml": repoCompose,
		"Chart.yaml":         "name: orders\ndependencies:\n  - name: redis\n    version: 18.x.x\n",
		"README.md":          "# not config",
	}))

	if !outcome.Complete {
		t.Errorf("outcome = %+v, want complete", outcome)
	}
	engines := map[string]bool{}
	for _, resource := range fact.Resources {
		engines[resource.Locator.Engine] = true
	}
	if !engines[derive.EnginePostgreSQL] {
		t.Errorf("resources = %+v, want the compose postgres", fact.Resources)
	}
	if !engines[derive.EngineRedis] {
		t.Errorf("resources = %+v, want the redis subchart", fact.Resources)
	}
}

// The credential discard has to hold all the way to the stored payload, because
// that is the thing that reaches the database.
func TestResourcesExtractor_FactNeverCarriesCredentials(t *testing.T) {
	fact, _ := extractResources(t, configInput(map[string]string{
		"docker-compose.yml":  repoCompose,
		"k8s/deployment.yaml": "kind: Deployment\nspec:\n  template:\n    spec:\n      containers:\n        - name: api\n          env:\n            - name: DATABASE_URL\n              value: postgres://root:hunter2@db.prod.internal:5432/orders\n",
	}))
	encoded, err := json.Marshal(fact)
	if err != nil {
		t.Fatalf("marshal fact: %v", err)
	}
	for _, secret := range []string{"hunter2", "secret", "root:", "svc:"} {
		if strings.Contains(string(encoded), secret) {
			t.Errorf("serialized fact contains %q: %s", secret, encoded)
		}
	}
}

// Both extractors read the same files, so the hosts half has to come out of the
// same pass without a second request budget.
func TestHostsExtractor_CollectsDeclaredAndConsumed(t *testing.T) {
	fact, outcome := extractHosts(t, configInput(map[string]string{
		"k8s/orders.yaml": `apiVersion: v1
kind: Service
metadata:
  name: orders-api
---
kind: Deployment
spec:
  template:
    spec:
      containers:
        - name: api
          env:
            - name: PAYMENTS_URL
              value: http://payments-api:8080
`,
	}))

	if !outcome.Complete {
		t.Errorf("outcome = %+v, want complete", outcome)
	}
	if len(fact.DeclaredServices) != 1 || fact.DeclaredServices[0] != "orders-api" {
		t.Errorf("declared_services = %v, want [orders-api]", fact.DeclaredServices)
	}
	if len(fact.ConsumedHosts) != 1 || fact.ConsumedHosts[0].Host != "payments-api" {
		t.Errorf("consumed_hosts = %+v, want payments-api", fact.ConsumedHosts)
	}
	if fact.ConsumedHosts[0].EnvVar != "PAYMENTS_URL" {
		t.Errorf("env_var = %q, want PAYMENTS_URL", fact.ConsumedHosts[0].EnvVar)
	}
}

// A templated Helm manifest legitimately fails to parse. It is an incompleteness —
// we do not know what it declares — but never an error, and never a panic.
func TestResourcesExtractor_TemplatedManifestIsIncompleteNotAPanic(t *testing.T) {
	_, outcome := extractResources(t, configInput(map[string]string{
		"k8s/deployment.yaml": "kind: Deployment\n{{- if .Values.enabled }}\nspec:\n  bad: [\n{{- end }}\n",
	}))
	if outcome.Complete {
		t.Error("outcome.Complete = true, want false after an unparseable manifest")
	}
	if !hasReason(outcome.Reasons, derive.ReasonParseFailed) {
		t.Errorf("reasons = %v, want parse_failed", outcome.Reasons)
	}
}

func TestResourcesExtractor_TooManyConfigFilesMarksIncomplete(t *testing.T) {
	contents := make(map[string]string, maxConfigFiles+5)
	for i := 0; i < maxConfigFiles+5; i++ {
		contents["k8s/overlays/env"+itoa(i)+"/service.yaml"] = "kind: Service\nmetadata:\n  name: svc-" + itoa(i) + "\n"
	}
	_, outcome := extractResources(t, configInput(contents))

	if outcome.Complete {
		t.Error("outcome.Complete = true, want false above the config-file ceiling")
	}
	if !hasReason(outcome.Reasons, derive.ReasonTooManyCandidates) {
		t.Errorf("reasons = %v, want too_many_candidates", outcome.Reasons)
	}
}

// ── reconciliation ───────────────────────────────────────────────────────────

func resourcesFact(t *testing.T, repositoryID string, complete bool, findings ...derive.ResourceFinding) models.RepositoryFact {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"data":    derive.ResourcesFact{Resources: findings},
		"outcome": derive.Outcome{Complete: complete},
	})
	if err != nil {
		t.Fatalf("marshal fact payload: %v", err)
	}
	return models.RepositoryFact{
		OrganizationID: testOrgID,
		RepositoryID:   repositoryID,
		FactKind:       models.FactKindResources,
		Payload:        encoded,
		Complete:       complete,
	}
}

func scopedFinding(engine, displayName string) derive.ResourceFinding {
	return derive.ResourceFinding{
		Locator:     derive.Locator{Engine: engine},
		Scoped:      true,
		DisplayName: displayName,
		RuleID:      derive.RuleResourceComposeImage,
		Confidence:  derive.ConfidenceLocalEvidence,
	}
}

func sharedFinding(engine, host string, portNumber int, namespace string) derive.ResourceFinding {
	p := portNumber
	return derive.ResourceFinding{
		Locator:     derive.Locator{Engine: engine, Host: host, Port: &p, Namespace: namespace},
		DisplayName: engine + " @ " + host + "/" + namespace,
		RuleID:      derive.RuleResourceDSN,
		Confidence:  derive.ConfidenceSharedLocator,
	}
}

func reconcileResourceFacts(t *testing.T, store *architectureStore, svc *ArchitectureService, facts ...models.RepositoryFact) {
	t.Helper()
	for i := range facts {
		fact := facts[i]
		store.facts[factKey(fact.RepositoryID, models.FactKindResources)] = &fact
	}
	if err := svc.Reconcile(context.Background(), testOrgID); err != nil {
		t.Fatalf("Reconcile() error = %v, want nil", err)
	}
}

// The scoped case, end to end: the resource belongs to one repository and the row
// says so.
func TestResourceReconcile_ScopedResourceBelongsToOneRepository(t *testing.T) {
	store := newArchitectureStore()
	svc := steppingService(store)

	reconcileResourceFacts(t, store, svc,
		resourcesFact(t, "repo-orders", true, scopedFinding(derive.EnginePostgreSQL, "db (local)")))

	resources := store.liveResources()
	if len(resources) != 1 {
		t.Fatalf("live resources = %d, want 1", len(resources))
	}
	if !resources[0].IsScoped() {
		t.Error("scoped_repository_id is nil, want the resource bound to its repository")
	}
	if *resources[0].ScopedRepositoryID != "repo-orders" {
		t.Errorf("scoped_repository_id = %q, want repo-orders", *resources[0].ScopedRepositoryID)
	}
	if len(store.liveLinks()) != 1 {
		t.Errorf("live links = %d, want the join row", len(store.liveLinks()))
	}
}

// Two repositories each with a local Postgres are two nodes. This is the rule that
// keeps the graph honest — `postgres` + `postgres` is not evidence of sharing.
func TestResourceReconcile_NeverUnifiesByEngineAlone(t *testing.T) {
	store := newArchitectureStore()
	svc := steppingService(store)

	reconcileResourceFacts(t, store, svc,
		resourcesFact(t, "repo-orders", true, scopedFinding(derive.EnginePostgreSQL, "db (local)")),
		resourcesFact(t, "repo-billing", true, scopedFinding(derive.EnginePostgreSQL, "db (local)")))

	if got := len(store.liveResources()); got != 2 {
		t.Errorf("live resources = %d, want 2 — two local databases are not one database", got)
	}
}

// The shared case: the same locator in two repositories genuinely is one resource,
// and it is the join that answers "do these two services use the same Postgres?".
func TestResourceReconcile_SameLocatorInTwoReposUnifies(t *testing.T) {
	store := newArchitectureStore()
	svc := steppingService(store)

	shared := sharedFinding(derive.EnginePostgreSQL, "db.prod.internal", 5432, "orders")
	reconcileResourceFacts(t, store, svc,
		resourcesFact(t, "repo-orders", true, shared),
		resourcesFact(t, "repo-billing", true, shared))

	resources := store.liveResources()
	if len(resources) != 1 {
		t.Fatalf("live resources = %d, want 1 shared node", len(resources))
	}
	if resources[0].IsScoped() {
		t.Error("scoped_repository_id is set, want NULL for a shared resource")
	}
	// Two consumers on one resource is the whole reason for the join table.
	if got := len(store.liveLinks()); got != 2 {
		t.Errorf("live links = %d, want one per consuming repository", got)
	}
}

// A repository that stops naming a resource loses its claim, and the resource
// itself is retired only once the last claim is gone.
func TestResourceReconcile_ResourceRetiredOnlyWhenTheLastClaimGoes(t *testing.T) {
	store := newArchitectureStore()
	svc := steppingService(store)

	shared := sharedFinding(derive.EnginePostgreSQL, "db.prod.internal", 5432, "orders")
	reconcileResourceFacts(t, store, svc,
		resourcesFact(t, "repo-orders", true, shared),
		resourcesFact(t, "repo-billing", true, shared))

	// billing stops using it; orders still does.
	reconcileResourceFacts(t, store, svc,
		resourcesFact(t, "repo-orders", true, shared),
		resourcesFact(t, "repo-billing", true))

	if got := len(store.liveResources()); got != 1 {
		t.Errorf("live resources = %d, want the resource kept while orders still names it", got)
	}
	if got := len(store.liveLinks()); got != 1 {
		t.Errorf("live links = %d, want only orders' claim", got)
	}

	// Now orders stops too.
	reconcileResourceFacts(t, store, svc,
		resourcesFact(t, "repo-orders", true),
		resourcesFact(t, "repo-billing", true))

	if got := len(store.liveResources()); got != 0 {
		t.Errorf("live resources = %d, want 0 once nothing references it", got)
	}
}

// The same completeness gate as everywhere else: a truncated tree or a rate limit
// cannot authorise deletion.
func TestResourceReconcile_IncompleteFactDoesNotSweep(t *testing.T) {
	store := newArchitectureStore()
	svc := steppingService(store)

	reconcileResourceFacts(t, store, svc,
		resourcesFact(t, "repo-orders", true, scopedFinding(derive.EnginePostgreSQL, "db (local)")))
	sweepsBefore := store.linkSweepCalls

	reconcileResourceFacts(t, store, svc, resourcesFact(t, "repo-orders", false))

	if store.linkSweepCalls != sweepsBefore {
		t.Errorf("link sweep calls = %d, want %d", store.linkSweepCalls, sweepsBefore)
	}
	if got := len(store.liveResources()); got != 1 {
		t.Errorf("live resources = %d, want the resource to survive", got)
	}
}

// A run where every fact was incomplete must not retire anything: with no
// authorised sweep, every live claim is unverified rather than absent.
func TestResourceReconcile_AllFactsIncompleteRetiresNothing(t *testing.T) {
	store := newArchitectureStore()
	svc := steppingService(store)

	reconcileResourceFacts(t, store, svc,
		resourcesFact(t, "repo-orders", true, scopedFinding(derive.EnginePostgreSQL, "db (local)")))
	retiresBefore := store.retireCalls

	reconcileResourceFacts(t, store, svc, resourcesFact(t, "repo-orders", false))

	if store.retireCalls != retiresBefore {
		t.Errorf("retire calls = %d, want %d — nothing was authorised to retract", store.retireCalls, retiresBefore)
	}
}

func TestResourceReconcile_IsIdempotentAcrossRuns(t *testing.T) {
	store := newArchitectureStore()
	svc := steppingService(store)

	fact := resourcesFact(t, "repo-orders", true, scopedFinding(derive.EnginePostgreSQL, "db (local)"))
	reconcileResourceFacts(t, store, svc, fact)
	firstID := store.liveResources()[0].ID
	reconcileResourceFacts(t, store, svc, fact)

	resources := store.liveResources()
	if len(resources) != 1 {
		t.Fatalf("live resources = %d, want 1", len(resources))
	}
	if resources[0].ID != firstID {
		t.Errorf("id = %q, want the original %q", resources[0].ID, firstID)
	}
}
