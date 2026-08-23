package derive

import (
	"sort"
	"strings"
)

// ResourceFinding is one resource a repository was shown to use, with the proof.
type ResourceFinding struct {
	Locator Locator `json:"locator"`
	// Scoped is true when the evidence identified an engine but not an instance
	// — a compose image, a Helm subchart. A scoped finding is never unified with
	// another repository's, because they are not the same resource. This is the
	// rule that keeps the graph from growing a single "Postgres" node with forty
	// edges arriving that corresponds to no database that exists.
	Scoped bool `json:"scoped"`
	// DisplayName is what the UI shows. For a scoped finding it says so out loud
	// ("postgres (local)"), because a person reading the graph has to be able to
	// tell inventory from topology.
	DisplayName string   `json:"display_name"`
	RuleID      string   `json:"rule_id"`
	Confidence  float64  `json:"confidence"`
	Evidence    []string `json:"evidence,omitempty"`
}

// HostFinding is one hostname a repository *consumes*, with the variable and file
// that named it.
//
// It lives in the resources extraction rather than its own pass because both come
// from the same files, and reading them twice would double the request budget for
// no gain. Phase 4 joins these against the Service names other repositories
// declare.
type HostFinding struct {
	Host string `json:"host"`
	// EnvVar is what lets the drawer say "via CHECKOUT_API_URL in
	// k8s/deployment.yaml", which is the only thing that makes a consumption edge
	// judgeable by a human in two seconds.
	EnvVar       string  `json:"env_var,omitempty"`
	EvidencePath string  `json:"evidence_path"`
	RuleID       string  `json:"rule_id"`
	Confidence   float64 `json:"confidence"`
	// FromTestFile marks a host found in a test or CI compose file. A repository
	// that mocks B in its tests is statically indistinguishable from one that
	// calls B, so which file it came from is the only signal available.
	FromTestFile bool `json:"from_test_file,omitempty"`
}

// ResourcesFact is the payload of a `resources` fact.
type ResourcesFact struct {
	Resources []ResourceFinding `json:"resources,omitempty"`
	// ComposeDependencies are intra-repository `depends_on` edges between
	// application services — phase 4's tier 1.
	ComposeDependencies []ComposeDependency `json:"compose_dependencies,omitempty"`
	// EngineHints are engines a repository clearly uses with no identity at all —
	// an empty `DATABASE_URL=` in an example file. They are metadata on the
	// repository, deliberately NOT graph nodes: a node with no locator is a
	// label pretending to be a thing.
	EngineHints []string `json:"engine_hints,omitempty"`
}

// ComposeDependency is one `depends_on` relation inside a compose file.
type ComposeDependency struct {
	From string `json:"from"`
	To   string `json:"to"`
	// ToEngine is set when the target service is an engine rather than an
	// application. That makes it an edge to a Resource, not to a Component, and
	// conflating the two would put a database in the service topology.
	ToEngine     string `json:"to_engine,omitempty"`
	EvidencePath string `json:"evidence_path"`
	FromTestFile bool   `json:"from_test_file,omitempty"`
}

// HostsFact is the payload of a `hosts` fact: what this repository declares, and
// what it consumes.
type HostsFact struct {
	// DeclaredServices are the Kubernetes `Service` names this repository owns.
	// In-cluster DNS is a naming *contract* — the name exists because someone
	// declared a Service with it — which is what makes matching against this list
	// a match against a declaration rather than a guess about naming.
	DeclaredServices []string `json:"declared_services,omitempty"`
	// DeclaredHosts are public hostnames from Ingress rules.
	DeclaredHosts []string `json:"declared_hosts,omitempty"`
	// ConsumedHosts are hostnames found in configuration values.
	ConsumedHosts []HostFinding `json:"consumed_hosts,omitempty"`
}

// dsnLikeKeys are the environment variable names whose *values* are worth parsing
// as connection strings.
//
// Note this is a filter on where to look, never on what to conclude: the value
// still has to parse as a DSN with a known scheme. The variable's *name* never
// implies anything, which is the line between this and the naming heuristic that
// phase 4 deliberately leaves out of v1.
func looksLikeConnectionKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, marker := range []string{"URL", "URI", "DSN", "CONNECTION", "CONN_STR", "ADDR", "HOST", "ENDPOINT", "BROKER", "SERVERS"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

// engineHintKeys map a bare variable name to the engine it implies.
//
// These produce a hint on the repository and never a node, because an empty
// `DATABASE_URL=` proves the repository talks to *a* database and identifies
// nothing. Rendering that as a graph node would be a label pretending to be a
// thing — the same tri-state discipline `internal/detect` uses for "has tests".
var engineHintKeys = map[string]string{
	"DATABASE_URL": EnginePostgreSQL, "DATABASE_URI": EnginePostgreSQL,
	"POSTGRES_URL": EnginePostgreSQL, "POSTGRESQL_URL": EnginePostgreSQL,
	"PG_URL": EnginePostgreSQL, "PGHOST": EnginePostgreSQL,
	"MYSQL_URL": EngineMySQL, "MYSQL_HOST": EngineMySQL,
	"REDIS_URL": EngineRedis, "REDIS_HOST": EngineRedis,
	"MONGO_URL": EngineMongoDB, "MONGODB_URI": EngineMongoDB,
	"KAFKA_BROKERS": EngineKafka, "KAFKA_BOOTSTRAP_SERVERS": EngineKafka,
	"AMQP_URL": EngineRabbitMQ, "RABBITMQ_URL": EngineRabbitMQ,
	"ELASTICSEARCH_URL": EngineElastic,
	"S3_BUCKET":         EngineS3, "AWS_S3_BUCKET": EngineS3,
}

// ResourceCollector accumulates findings across a repository's config files.
//
// It is a collector rather than a pure function per file because agreement has to
// be resolved across files: the same resource can show up in a compose file and in
// a values.yaml, and the rule for that is take the max confidence and keep both
// pieces of evidence. Never multiply and never average — two rules reading the
// same docker-compose.yml share a root cause, and combining correlated signals
// inflates the number.
type ResourceCollector struct {
	resources map[string]*ResourceFinding
	deps      []ComposeDependency
	hints     map[string]bool

	declaredServices map[string]bool
	declaredHosts    map[string]bool
	consumed         map[string]*HostFinding
}

func NewResourceCollector() *ResourceCollector {
	return &ResourceCollector{
		resources:        map[string]*ResourceFinding{},
		hints:            map[string]bool{},
		declaredServices: map[string]bool{},
		declaredHosts:    map[string]bool{},
		consumed:         map[string]*HostFinding{},
	}
}

// resourceKey is a finding's identity within one repository's collection.
//
// A scoped finding is keyed by its display name so two local engines of the same
// kind stay distinct, while a shared one is keyed by the locator so the same host
// seen twice merges instead of duplicating.
func resourceKey(finding ResourceFinding) string {
	if finding.Scoped {
		return "scoped\x00" + finding.Locator.Engine + "\x00" + finding.DisplayName
	}
	port := ""
	if finding.Locator.Port != nil {
		port = itoa(*finding.Locator.Port)
	}
	return strings.Join([]string{"shared", finding.Locator.Engine, finding.Locator.Host, port, finding.Locator.Namespace}, "\x00")
}

// AddResource records a finding, merging it with an equal one.
func (c *ResourceCollector) AddResource(finding ResourceFinding, evidencePath string) {
	if finding.Locator.Engine == "" {
		return
	}
	key := resourceKey(finding)
	existing, found := c.resources[key]
	if !found {
		finding.Evidence = nil
		copied := finding
		copied.appendEvidence(evidencePath)
		c.resources[key] = &copied
		return
	}
	// Agreement takes the max and keeps both pieces of evidence.
	if finding.Confidence > existing.Confidence {
		existing.Confidence = finding.Confidence
		existing.RuleID = finding.RuleID
	}
	// A locator that gained a real host stops being scoped: the stronger evidence
	// wins outright rather than averaging with the weaker one.
	if !finding.Scoped && existing.Scoped {
		existing.Scoped = false
		existing.Locator = finding.Locator
		existing.DisplayName = finding.DisplayName
	}
	existing.appendEvidence(evidencePath)
}

func (f *ResourceFinding) appendEvidence(path string) {
	if path == "" || len(f.Evidence) >= maxEvidence {
		return
	}
	for _, existing := range f.Evidence {
		if existing == path {
			return
		}
	}
	f.Evidence = append(f.Evidence, path)
}

// AddEngineHint records that the repository uses an engine with no identity.
func (c *ResourceCollector) AddEngineHint(engine string) {
	if engine != "" {
		c.hints[engine] = true
	}
}

// AddComposeDependency records one intra-repository `depends_on` relation.
func (c *ResourceCollector) AddComposeDependency(dep ComposeDependency) {
	c.deps = append(c.deps, dep)
}

// AddDeclaredService records a Kubernetes Service name this repository owns.
func (c *ResourceCollector) AddDeclaredService(name string) {
	if name != "" && !IsPlaceholderHost(name) {
		c.declaredServices[strings.ToLower(name)] = true
	}
}

// AddDeclaredHost records a public hostname this repository claims.
func (c *ResourceCollector) AddDeclaredHost(host string) {
	if host != "" && !IsPlaceholderHost(host) {
		c.declaredHosts[strings.ToLower(host)] = true
	}
}

// AddConsumedService records a compose `depends_on` target that is not an engine.
//
// This is tier 1's only cross-repository expression. A compose service name is a
// *local alias*, not a DNS contract — the repository chose it — so on its own it
// proves nothing about another repository. It becomes an edge only if some other
// repository declares a Kubernetes Service with that exact name, and it carries a
// lower tier than a name read out of an environment value for exactly that reason.
//
// The intra-repository pairs stay in the resources fact as topology, because
// `Component != Repository` is out of scope: there is no node for "the worker
// inside this repository" to point at, and fabricating a self-edge would be worse
// than recording the fact and waiting for the node type to exist.
func (c *ResourceCollector) AddConsumedService(name, evidencePath string, fromTestFile bool) {
	c.AddConsumedHost(HostFinding{
		Host:         name,
		EvidencePath: evidencePath,
		RuleID:       RuleConsumeComposeDependsOn,
		Confidence:   ConfidenceComposeTopology,
		FromTestFile: fromTestFile,
	})
}

// AddConsumedHost records a hostname found in a configuration value.
//
// A placeholder is discarded here without evidence and without marking anything
// incomplete: `.env.example` is *made* of placeholders, and a value that
// legitimately points at nothing is a decision, not a failure.
func (c *ResourceCollector) AddConsumedHost(finding HostFinding) {
	finding.Host = strings.ToLower(strings.TrimSpace(finding.Host))
	if finding.Host == "" || IsPlaceholderHost(finding.Host) {
		return
	}
	key := finding.Host + "\x00" + finding.EnvVar
	existing, found := c.consumed[key]
	if !found {
		copied := finding
		c.consumed[key] = &copied
		return
	}
	if finding.Confidence > existing.Confidence {
		existing.Confidence = finding.Confidence
		existing.RuleID = finding.RuleID
	}
	// A host seen in a production file as well as a test one is no longer
	// test-only, and that is the fact the drawer needs.
	if !finding.FromTestFile {
		existing.FromTestFile = false
		existing.EvidencePath = finding.EvidencePath
	}
}

// ScanEnvironment reads one file's environment values for DSNs, hostnames and
// engine hints.
func (c *ResourceCollector) ScanEnvironment(env map[string]string, evidencePath string, fromTestFile bool) {
	for key, value := range env {
		if engine, hinted := engineHintKeys[strings.ToUpper(key)]; hinted {
			c.AddEngineHint(engine)
		}
		if value == "" || !looksLikeConnectionKey(key) {
			continue
		}

		// A DSN with a real host is the only evidence that can unify two
		// repositories, so it is the strongest rule here.
		if locator, ok := ParseDSN(value); ok {
			if locator.IsShared() {
				c.AddResource(ResourceFinding{
					Locator:     locator,
					DisplayName: locator.displayName(),
					RuleID:      RuleResourceDSN,
					Confidence:  ConfidenceSharedLocator,
				}, evidencePath)
			} else {
				// The DSN parsed but pointed at localhost or a placeholder: the
				// engine is real, the instance is not.
				c.AddEngineHint(locator.Engine)
			}
			continue
		}

		// Not a DSN: it may still be a bare hostname or an HTTP URL naming
		// another service, which is phase 4's input.
		if host, ok := hostFromValue(value); ok {
			c.AddConsumedHost(HostFinding{
				Host:         host,
				EnvVar:       key,
				EvidencePath: evidencePath,
				RuleID:       RuleConsumeK8sServiceHost,
				Confidence:   ConfidenceDeclaredHost,
				FromTestFile: fromTestFile,
			})
		}
	}
}

// displayName renders a locator for a person.
func (l Locator) displayName() string {
	if l.Host == "" {
		return l.Engine
	}
	name := l.Engine + " @ " + l.Host
	if l.Namespace != "" {
		name += "/" + l.Namespace
	}
	return name
}

// hostFromValue pulls a hostname out of a configuration value.
//
// It accepts an http(s) URL and a bare `host:port`, and rejects anything else. In
// particular it refuses a value with a template marker: `svc-${ENV}.internal`
// names no host that can be joined against a declaration.
func hostFromValue(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, " \t\"'") {
		return "", false
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		rest := value[strings.Index(value, "//")+2:]
		// Strip any userinfo before the host, so a credential cannot leak into a
		// hostname the way it could into a DSN.
		if at := strings.LastIndex(rest, "@"); at >= 0 {
			rest = rest[at+1:]
		}
		rest = strings.SplitN(rest, "/", 2)[0]
		rest = strings.SplitN(rest, ":", 2)[0]
		return normalizeHost(rest)
	}
	if strings.Contains(value, "://") {
		// Some other scheme entirely — a DSN we do not recognize, or a git URL.
		return "", false
	}
	// A bare value is accepted as a host only when it carries a port
	// (`orders-api:8080`) or is a dotted name (`orders.prod.svc.cluster.local`).
	// Both forms appear in the tier-2a examples. A bare single word with neither
	// is refused: `info`, `production` and `true` are far more likely than a
	// hostname, and there is no way to tell them apart.
	host, port, hasPort := strings.Cut(value, ":")
	if hasPort {
		if port == "" {
			return "", false
		}
		if _, err := parsePort(port); err != nil {
			return "", false
		}
		return normalizeHost(host)
	}
	if !strings.Contains(value, ".") {
		return "", false
	}
	return normalizeHost(value)
}

// normalizeHost lowercases a hostname and rejects anything that is not one.
//
// The letter requirement is what keeps a version string out: `3.11` is dotted and
// would otherwise pass as a host, and an index entry for it would match nothing
// while polluting every evidence list that mentioned it.
func normalizeHost(host string) (string, bool) {
	host = strings.ToLower(strings.Trim(strings.TrimSpace(host), "."))
	if host == "" || IsPlaceholderHost(host) {
		return "", false
	}
	hasLetter := false
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z':
			hasLetter = true
		case r >= '0' && r <= '9', r == '.', r == '-', r == '_':
		default:
			return "", false
		}
	}
	if !hasLetter {
		return "", false
	}
	return host, true
}

// ServiceLabel is the first DNS label of an in-cluster hostname.
//
// `orders.prod.svc.cluster.local` and `orders-api:8080` both reduce to the name
// someone actually declared a Service with, which is what makes the match a match
// against a declaration.
func ServiceLabel(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if idx := strings.Index(host, ":"); idx > 0 {
		host = host[:idx]
	}
	if idx := strings.Index(host, "."); idx > 0 {
		return host[:idx]
	}
	return host
}

// Resources returns the collected findings in a stable order.
func (c *ResourceCollector) Resources() []ResourceFinding {
	out := make([]ResourceFinding, 0, len(c.resources))
	for _, finding := range c.resources {
		out = append(out, *finding)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Locator.Engine != out[j].Locator.Engine {
			return out[i].Locator.Engine < out[j].Locator.Engine
		}
		return out[i].DisplayName < out[j].DisplayName
	})
	return out
}

// ResourcesFact renders the collected resource half.
func (c *ResourceCollector) ResourcesFact() ResourcesFact {
	hints := make([]string, 0, len(c.hints))
	for engine := range c.hints {
		hints = append(hints, engine)
	}
	sort.Strings(hints)

	deps := append([]ComposeDependency(nil), c.deps...)
	sort.Slice(deps, func(i, j int) bool {
		if deps[i].From != deps[j].From {
			return deps[i].From < deps[j].From
		}
		return deps[i].To < deps[j].To
	})

	return ResourcesFact{
		Resources:           c.Resources(),
		ComposeDependencies: deps,
		EngineHints:         hints,
	}
}

// HostsFact renders the collected host half.
func (c *ResourceCollector) HostsFact() HostsFact {
	fact := HostsFact{}
	for name := range c.declaredServices {
		fact.DeclaredServices = append(fact.DeclaredServices, name)
	}
	for host := range c.declaredHosts {
		fact.DeclaredHosts = append(fact.DeclaredHosts, host)
	}
	sort.Strings(fact.DeclaredServices)
	sort.Strings(fact.DeclaredHosts)

	for _, finding := range c.consumed {
		fact.ConsumedHosts = append(fact.ConsumedHosts, *finding)
	}
	sort.Slice(fact.ConsumedHosts, func(i, j int) bool {
		if fact.ConsumedHosts[i].Host != fact.ConsumedHosts[j].Host {
			return fact.ConsumedHosts[i].Host < fact.ConsumedHosts[j].Host
		}
		return fact.ConsumedHosts[i].EnvVar < fact.ConsumedHosts[j].EnvVar
	})
	return fact
}
