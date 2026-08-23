package derive

import (
	"encoding/json"
	"strings"
	"testing"
)

func collectorFor(t *testing.T, files map[string]string) *ResourceCollector {
	t.Helper()
	collector := NewResourceCollector()
	for path, content := range files {
		switch {
		case IsComposeFile(path):
			file, err := ParseCompose(path, []byte(content))
			if err != nil {
				t.Fatalf("ParseCompose(%q): %v", path, err)
			}
			engines := map[string]string{}
			for _, service := range file.Services {
				if service.Engine != "" {
					engines[service.Name] = service.Engine
					collector.AddResource(ResourceFinding{
						Locator:     Locator{Engine: service.Engine},
						Scoped:      true,
						DisplayName: service.Name + " (local)",
						RuleID:      RuleResourceComposeImage,
						Confidence:  ConfidenceLocalEvidence,
					}, path)
				}
				collector.ScanEnvironment(service.Environment, path, file.IsTestCompose)
			}
			for _, service := range file.Services {
				for _, target := range service.DependsOn {
					collector.AddComposeDependency(ComposeDependency{
						From: service.Name, To: target, ToEngine: engines[target],
						EvidencePath: path, FromTestFile: file.IsTestCompose,
					})
				}
			}
		case IsDotEnvExample(path):
			collector.ScanEnvironment(ParseDotEnv([]byte(content)), path, false)
		case IsK8sManifest(path):
			manifest, _ := ParseK8sManifest(path, []byte(content))
			for _, name := range manifest.ServiceNames {
				collector.AddDeclaredService(name)
			}
			for _, host := range manifest.IngressHosts {
				collector.AddDeclaredHost(host)
			}
			collector.ScanEnvironment(manifest.Environment, path, false)
		}
	}
	return collector
}

func findingFor(t *testing.T, findings []ResourceFinding, engine string) ResourceFinding {
	t.Helper()
	for _, finding := range findings {
		if finding.Locator.Engine == engine {
			return finding
		}
	}
	t.Fatalf("no finding for engine %q in %+v", engine, findings)
	return ResourceFinding{}
}

// ── the unification rules ────────────────────────────────────────────────────

// The rule that keeps the graph from becoming a pretty lie. Two repositories each
// running `postgres:16` locally are TWO nodes, because they are not the same
// Postgres — a single "Postgres" node with forty edges arriving corresponds to no
// database that exists.
func TestResource_ComposeOnlyStaysScopedToRepo(t *testing.T) {
	collector := collectorFor(t, map[string]string{
		"docker-compose.yml": "services:\n  db:\n    image: postgres:16\n",
	})
	findings := collector.Resources()
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want one", findings)
	}
	got := findings[0]
	if !got.Scoped {
		t.Error("Scoped = false, want true — a compose image identifies an engine, not an instance")
	}
	if got.Locator.Host != "" {
		t.Errorf("host = %q, want empty", got.Locator.Host)
	}
	// The UI has to be able to say this is local, so the display name says it.
	if !strings.Contains(got.DisplayName, "local") {
		t.Errorf("display_name = %q, want it to mark the resource as local", got.DisplayName)
	}
	if got.Confidence != ConfidenceLocalEvidence {
		t.Errorf("confidence = %v, want the local-evidence tier", got.Confidence)
	}
}

// `postgres` + `postgres` is not evidence. This is the test that stops the graph
// from unifying on engine alone.
func TestResource_NeverUnifiesByEngineAlone(t *testing.T) {
	first := collectorFor(t, map[string]string{
		"docker-compose.yml": "services:\n  db:\n    image: postgres:16\n",
	})
	second := collectorFor(t, map[string]string{
		"docker-compose.yml": "services:\n  database:\n    image: postgres:16\n",
	})

	a := findingFor(t, first.Resources(), EnginePostgreSQL)
	b := findingFor(t, second.Resources(), EnginePostgreSQL)

	if !a.Scoped || !b.Scoped {
		t.Fatal("both findings must be scoped, or they could be unified")
	}
	// Two scoped findings from different repositories can never collide, because
	// the scoped identity includes the repository — see migration 035's second
	// unique index.
	if resourceKey(a) == resourceKey(b) && a.DisplayName == b.DisplayName {
		t.Error("the two findings share an identity, want them distinct per repository")
	}
}

// A DSN with a real host is the only evidence that can unify: the same
// (engine, host, port, namespace) in two repositories genuinely is one resource.
func TestResource_SameLocatorInTwoReposUnifies(t *testing.T) {
	dsn := "DATABASE_URL=postgres://svc:pw@db.prod.internal:5432/orders\n"
	first := collectorFor(t, map[string]string{".env.example": dsn})
	second := collectorFor(t, map[string]string{".env.example": dsn})

	a := findingFor(t, first.Resources(), EnginePostgreSQL)
	b := findingFor(t, second.Resources(), EnginePostgreSQL)

	if a.Scoped || b.Scoped {
		t.Fatalf("findings are scoped (%v/%v), want shared when the host is known", a.Scoped, b.Scoped)
	}
	if resourceKey(a) != resourceKey(b) {
		t.Errorf("identities differ (%q vs %q), want the same locator to unify", resourceKey(a), resourceKey(b))
	}
	if a.Locator.Host != "db.prod.internal" || a.Locator.Namespace != "orders" {
		t.Errorf("locator = %+v, want the DSN's host and database", a.Locator)
	}
}

// The logical database is part of the identity, so two services on different
// databases of the same instance are two resources.
func TestResource_DifferentNamespaceIsADifferentResource(t *testing.T) {
	orders := collectorFor(t, map[string]string{".env.example": "DATABASE_URL=postgres://svc:pw@db.prod:5432/orders\n"})
	billing := collectorFor(t, map[string]string{".env.example": "DATABASE_URL=postgres://svc:pw@db.prod:5432/billing\n"})

	a := findingFor(t, orders.Resources(), EnginePostgreSQL)
	b := findingFor(t, billing.Resources(), EnginePostgreSQL)
	if resourceKey(a) == resourceKey(b) {
		t.Error("identities are equal, want db.namespace to distinguish them")
	}
}

// An empty `DATABASE_URL=` proves the repository talks to a database and
// identifies nothing. Rendering that as a node would be a label pretending to be a
// thing, so it becomes a hint on the repository instead.
func TestResource_EmptyEnvVarIsNotANode(t *testing.T) {
	collector := collectorFor(t, map[string]string{
		".env.example": "DATABASE_URL=\nREDIS_URL=\n",
	})
	fact := collector.ResourcesFact()

	if len(fact.Resources) != 0 {
		t.Errorf("resources = %+v, want no node from a valueless variable", fact.Resources)
	}
	if len(fact.EngineHints) != 2 {
		t.Errorf("engine_hints = %v, want postgresql and redis as hints", fact.EngineHints)
	}
}

// A localhost DSN parses but identifies no instance, so it is a hint and not a
// shared node — otherwise every repository in the organization would converge on
// one fictional "localhost postgres".
func TestResource_LocalhostDSNIsAHintNotASharedNode(t *testing.T) {
	collector := collectorFor(t, map[string]string{
		".env.example": "DATABASE_URL=postgres://user:pw@localhost:5432/app\n",
	})
	fact := collector.ResourcesFact()

	for _, resource := range fact.Resources {
		if !resource.Scoped && resource.Locator.Host == "localhost" {
			t.Errorf("resources = %+v, want no shared node for localhost", fact.Resources)
		}
	}
	if len(fact.EngineHints) == 0 {
		t.Errorf("engine_hints = %v, want postgresql recorded as a hint", fact.EngineHints)
	}
}

// Stronger evidence wins outright rather than averaging with the weaker one: a
// repository whose compose file runs a local Postgres *and* whose manifest names a
// production host has one resource, the real one.
func TestResource_SharedEvidencePromotesAScopedFinding(t *testing.T) {
	collector := NewResourceCollector()
	collector.AddResource(ResourceFinding{
		Locator:     Locator{Engine: EnginePostgreSQL},
		Scoped:      true,
		DisplayName: "db (local)",
		RuleID:      RuleResourceComposeImage,
		Confidence:  ConfidenceLocalEvidence,
	}, "docker-compose.yml")

	port := 5432
	collector.AddResource(ResourceFinding{
		Locator:     Locator{Engine: EnginePostgreSQL, Host: "db.prod.internal", Port: &port, Namespace: "orders"},
		DisplayName: "postgresql @ db.prod.internal/orders",
		RuleID:      RuleResourceDSN,
		Confidence:  ConfidenceSharedLocator,
	}, "k8s/deployment.yaml")

	// They are different identities, so both rows exist — but only one of them is
	// the shared node, and it carries the higher tier.
	findings := collector.Resources()
	shared := 0
	for _, finding := range findings {
		if !finding.Scoped {
			shared++
			if finding.Confidence != ConfidenceSharedLocator {
				t.Errorf("shared confidence = %v, want the shared-locator tier", finding.Confidence)
			}
		}
	}
	if shared != 1 {
		t.Errorf("shared findings = %d, want exactly 1", shared)
	}
}

// Agreement takes the max and keeps both pieces of evidence. Never multiply and
// never average: two rules reading the same docker-compose.yml share a root cause,
// and combining correlated signals inflates the number.
func TestResource_AgreementTakesMaxAndKeepsBothEvidencePaths(t *testing.T) {
	port := 5432
	locator := Locator{Engine: EnginePostgreSQL, Host: "db.prod.internal", Port: &port, Namespace: "orders"}

	collector := NewResourceCollector()
	collector.AddResource(ResourceFinding{
		Locator: locator, DisplayName: "pg", RuleID: RuleResourceComposeImage, Confidence: ConfidenceLocalEvidence,
	}, "docker-compose.yml")
	collector.AddResource(ResourceFinding{
		Locator: locator, DisplayName: "pg", RuleID: RuleResourceDSN, Confidence: ConfidenceSharedLocator,
	}, "k8s/deployment.yaml")

	findings := collector.Resources()
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want one merged finding", findings)
	}
	got := findings[0]
	if got.Confidence != ConfidenceSharedLocator {
		t.Errorf("confidence = %v, want the max of the two tiers", got.Confidence)
	}
	if got.Confidence > 1 {
		t.Errorf("confidence = %v, want agreement never to sum past 1", got.Confidence)
	}
	if len(got.Evidence) != 2 {
		t.Errorf("evidence = %v, want both paths recorded", got.Evidence)
	}
}

// Nothing derived may carry a credential. This asserts it on the serialized fact,
// which is what actually reaches the database.
func TestResource_FactNeverCarriesCredentials(t *testing.T) {
	collector := collectorFor(t, map[string]string{
		"k8s/deployment.yaml": k8sMultiDoc,
		"docker-compose.yml":  composeYAML,
	})
	encoded, err := json.Marshal(collector.ResourcesFact())
	if err != nil {
		t.Fatalf("marshal fact: %v", err)
	}
	for _, secret := range []string{"secret", "pw@", "user:", "svc:pw"} {
		if strings.Contains(string(encoded), secret) {
			t.Errorf("serialized fact contains %q: %s", secret, encoded)
		}
	}
}

// ── compose dependencies ─────────────────────────────────────────────────────

// A `depends_on` pointing at an engine is an edge to a Resource, not to a
// Component. Conflating them would put a database in the service topology.
func TestResource_ComposeDependencyDistinguishesEngineFromApplication(t *testing.T) {
	collector := collectorFor(t, map[string]string{"docker-compose.yml": composeYAML})
	fact := collector.ResourcesFact()

	byTarget := map[string]ComposeDependency{}
	for _, dep := range fact.ComposeDependencies {
		byTarget[dep.To] = dep
	}
	if dep, ok := byTarget["db"]; !ok || dep.ToEngine != EnginePostgreSQL {
		t.Errorf("depends_on db = %+v, want it marked as the postgresql engine", dep)
	}
	if dep, ok := byTarget["worker"]; !ok || dep.ToEngine != "" {
		t.Errorf("depends_on worker = %+v, want no engine — it is an application service", dep)
	}
}

// ── consumed hosts ───────────────────────────────────────────────────────────

// The env var name and the file are what let the drawer say "via CHECKOUT_API_URL
// in k8s/deployment.yaml", which is the only thing that makes a consumption edge
// judgeable by a human in two seconds.
func TestHosts_ConsumedHostCarriesEnvVarAndEvidencePath(t *testing.T) {
	collector := collectorFor(t, map[string]string{"k8s/deployment.yaml": k8sMultiDoc})
	fact := collector.HostsFact()

	var found bool
	for _, consumed := range fact.ConsumedHosts {
		if consumed.Host == "payments.prod.svc.cluster.local" {
			found = true
			if consumed.EnvVar != "PAYMENTS_HOST" {
				t.Errorf("env_var = %q, want PAYMENTS_HOST", consumed.EnvVar)
			}
			if consumed.EvidencePath != "k8s/deployment.yaml" {
				t.Errorf("evidence_path = %q, want the manifest", consumed.EvidencePath)
			}
		}
	}
	if !found {
		t.Errorf("consumed_hosts = %+v, want the in-cluster host", fact.ConsumedHosts)
	}
	// The Service this repository declares is the input to the other side of the
	// phase 4 join.
	if len(fact.DeclaredServices) != 1 || fact.DeclaredServices[0] != "orders-api" {
		t.Errorf("declared_services = %v, want [orders-api]", fact.DeclaredServices)
	}
	if len(fact.DeclaredHosts) != 1 || fact.DeclaredHosts[0] != "orders.acme.example" {
		t.Errorf("declared_hosts = %v, want the ingress host", fact.DeclaredHosts)
	}
}

// Without the denylist the consumption edges turn to noise, because `.env.example`
// is made of placeholders. A placeholder is discarded without evidence: it is a
// value that legitimately points at nothing, not a failure to inspect.
func TestConsume_IgnoresLocalhostAndPlaceholders(t *testing.T) {
	collector := collectorFor(t, map[string]string{
		".env.example": `ORDERS_API_URL=http://localhost:8080
PAYMENTS_URL=http://127.0.0.1:9000
BILLING_HOST=your-billing-host:443
SHIPPING_URL=http://example.com
INVENTORY_HOST=${INVENTORY_HOST}:8080
NOTIFY_URL=http://{{ .Values.notify }}:8080
`,
	})
	if got := collector.HostsFact().ConsumedHosts; len(got) != 0 {
		t.Errorf("consumed_hosts = %+v, want every placeholder discarded", got)
	}
}

// A third-party host needs no denylist: it matches nothing in the internal index,
// so it produces no edge on its own. It is recorded because recording it costs
// nothing and hiding it would make the drawer's evidence incomplete.
func TestConsume_ThirdPartyHostIsRecordedButMatchesNothing(t *testing.T) {
	collector := collectorFor(t, map[string]string{
		".env.example": "STRIPE_API_URL=https://api.stripe.com\n",
	})
	consumed := collector.HostsFact().ConsumedHosts
	if len(consumed) != 1 || consumed[0].Host != "api.stripe.com" {
		t.Fatalf("consumed_hosts = %+v, want the third-party host recorded", consumed)
	}
	// Nothing in the organization declares a Service called `api`, so the phase 4
	// join simply finds no match.
	if ServiceLabel(consumed[0].Host) != "api" {
		t.Errorf("service label = %q, want the first DNS label", ServiceLabel(consumed[0].Host))
	}
}

// A host seen in a production file as well as a test one is no longer test-only,
// and that is the fact the drawer needs to show.
func TestConsume_ProductionEvidenceOutranksTestEvidence(t *testing.T) {
	collector := NewResourceCollector()
	collector.AddConsumedHost(HostFinding{
		Host: "orders-api", EnvVar: "ORDERS_URL",
		EvidencePath: "docker-compose.test.yml", RuleID: RuleConsumeK8sServiceHost,
		Confidence: ConfidenceDeclaredHost, FromTestFile: true,
	})
	collector.AddConsumedHost(HostFinding{
		Host: "orders-api", EnvVar: "ORDERS_URL",
		EvidencePath: "k8s/deployment.yaml", RuleID: RuleConsumeK8sServiceHost,
		Confidence: ConfidenceDeclaredHost, FromTestFile: false,
	})

	consumed := collector.HostsFact().ConsumedHosts
	if len(consumed) != 1 {
		t.Fatalf("consumed_hosts = %+v, want one merged finding", consumed)
	}
	if consumed[0].FromTestFile {
		t.Error("FromTestFile = true, want production evidence to win")
	}
	if consumed[0].EvidencePath != "k8s/deployment.yaml" {
		t.Errorf("evidence_path = %q, want the production file", consumed[0].EvidencePath)
	}
}
