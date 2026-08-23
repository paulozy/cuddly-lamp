package services

import (
	"context"
	"strings"

	"github.com/paulozy/idp-with-ai-backend/internal/derive"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// consumeDeriver turns `hosts` facts into "A calls B" edges.
//
// Worth knowing before reading the rules: the incumbents do not do this. In
// Backstage `consumesApis` is *declared* by a human in `catalog-info.yaml` — the
// well-known-relations doc says the relation is "commonly generated based on
// spec.consumesApis of the component", i.e. generated from the declaration, not
// discovered. OpsLevel and Cortex take service dependencies from an integration or
// manual entry. Everything that delivers this reliably derives it from runtime
// telemetry: Kiali, Weave Scope, Datadog, Dash0, OTel's `spanmetrics`.
//
// So deriving it from committed config is doing something the incumbents avoid.
// The control is limiting the tiers, and the most tempting tier is the one left
// out:
//
//	tier 1  — compose `depends_on`, 0.75. High precision, very low recall, and
//	          intra-repository only. A compose file describes what that repository
//	          brings up, which is still a real statement about its topology.
//	tier 2a — a Kubernetes Service DNS name in an environment *value*, 0.85. The
//	          best static signal there is, because in-cluster DNS is a naming
//	          *contract*: the name exists because someone declared a Service with
//	          it, so the match is against a declaration and not against a guess.
//	tier 2b — public hostname against an Ingress map. Included, and it works only
//	          because the map comes from Ingress rules the repositories themselves
//	          declare — the same "match a declaration" property as 2a.
//	tier 3  — fuzzy matching on the env var *name* (`ORDERS_API_URL` → repository
//	          `orders`). NOT IMPLEMENTED. A variable's name reflects what a team
//	          calls a service, not a declaration: it may point at a third party, a
//	          mock, a gateway, or nothing. It is the tier that generates the noise.
//
// And the limitation with no fix: a repository that mocks B in its tests is
// statically indistinguishable from one that calls B. A
// `docker-compose.test.yml` standing up a stub of `orders-api` produces the same
// text as a compose file consuming the real one. Only runtime telemetry tells them
// apart — which is very likely *why* Backstage left `consumesApis` as a human
// declaration. Two consequences: these edges always render in the derived bucket,
// never as declarations, and the drawer carries the exact evidence path so a person
// can judge in two seconds whether it is a mock.
type consumeDeriver struct{}

const consumeVersion = "v1"

func (consumeDeriver) Key(organizationID string) string {
	return "apiconsume:" + consumeVersion + ":org/" + organizationID
}

func (consumeDeriver) FactKind() models.RepositoryFactKind { return models.FactKindHosts }

// hostIndex maps a declared name to the repository that declared it.
//
// Same ambiguity rule as the package index in phase 1: two repositories declaring
// the same Service name produce no edge at all. Guessing there would be a wrong
// edge at high confidence, which is worse than a missing one.
type hostIndex struct {
	owners map[string]map[string]bool
}

func newHostIndex() *hostIndex {
	return &hostIndex{owners: map[string]map[string]bool{}}
}

func (idx *hostIndex) add(name, repositoryID string) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || repositoryID == "" || derive.IsPlaceholderHost(name) {
		return
	}
	if idx.owners[name] == nil {
		idx.owners[name] = map[string]bool{}
	}
	idx.owners[name][repositoryID] = true
}

func (idx *hostIndex) resolve(name string) (string, bool) {
	owners := idx.owners[strings.ToLower(strings.TrimSpace(name))]
	if len(owners) != 1 {
		// Zero owners means nothing internal declares it — a third-party host, and
		// it simply produces nothing. More than one is a contest, and the answer
		// there is also nothing.
		return "", false
	}
	for id := range owners {
		return id, true
	}
	return "", false
}

func (d consumeDeriver) Derive(_ context.Context, _ string, facts []models.RepositoryFact) (DerivedSet, error) {
	set := DerivedSet{Complete: true}

	services := newHostIndex()
	ingress := newHostIndex()
	payloads := make(map[string]derive.HostsFact, len(facts))

	for i := range facts {
		var payload derive.HostsFact
		outcome := UnmarshalFactPayload(&facts[i], &payload)
		if !outcome.Complete {
			// Org-wide index, so one repository's incomplete extraction withdraws
			// the sweep for everyone: a Service name it failed to declare hides an
			// edge from every other repository to it.
			set.Complete = false
			for _, reason := range outcome.Reasons {
				set.Reasons = appendUnique(set.Reasons, reason)
			}
		}
		payloads[facts[i].RepositoryID] = payload
		for _, name := range payload.DeclaredServices {
			services.add(name, facts[i].RepositoryID)
		}
		for _, host := range payload.DeclaredHosts {
			ingress.add(host, facts[i].RepositoryID)
		}
	}

	// agreement holds one edge per (source, target), so two tiers naming the same
	// pair take the max and keep both pieces of evidence rather than producing two
	// rows or a summed number.
	agreement := map[string]*DerivedEdge{}

	for repositoryID, payload := range payloads {
		for _, consumed := range payload.ConsumedHosts {
			targetID, ruleID, confidence, ok := d.match(consumed, services, ingress)
			if !ok || targetID == repositoryID {
				// A self-edge is the provider repository shipping a client of
				// itself in its own compose file — real, and not an edge.
				continue
			}

			kind := models.RepositoryRelationshipKindHTTP
			if isBrokerHost(consumed) {
				kind = models.RepositoryRelationshipKindAsync
			}
			// A host found only in a test file is still recorded, at the tier the
			// rule earns, because the drawer's job is to let a person judge it. The
			// rule id says which file won, so "this is a mock" is visible.
			effectiveRule := ruleID
			if consumed.FromTestFile {
				effectiveRule = ruleID + ".test_file"
			}

			mergeConsumeEdge(agreement, DerivedEdge{
				SourceRepositoryID: repositoryID,
				TargetRepositoryID: targetID,
				Kind:               kind,
				Source:             models.RepositoryRelationshipSourceConfig,
				Confidence:         confidence,
				Label:              consumed.Host,
				Fingerprint: Fingerprint(effectiveRule, consumed.Host,
					consumed.EvidencePath, repositoryID, targetID),
				Metadata: map[string]any{
					"rule_id":        effectiveRule,
					"matched_host":   consumed.Host,
					"evidence_path":  consumed.EvidencePath,
					"env_var_name":   consumed.EnvVar,
					"tier":           tierFor(ruleID),
					"from_test_file": consumed.FromTestFile,
				},
			})
		}
	}

	for _, edge := range agreement {
		set.Edges = append(set.Edges, *edge)
	}
	utils.Info("architecture: derived consumption edges", "count", len(set.Edges))
	return set, nil
}

// match resolves a consumed host against what the organization declares.
//
// The tier is the *pair* of how the name was found and what it matched, because
// both halves carry information:
//
//	env value  + declared Service → tier 2a, 0.85. The value is a DNS name and the
//	                                name is a contract someone declared.
//	env value  + declared Ingress → tier 2b, 0.75. A public hostname, matched only
//	                                against a rule a repository actually declared.
//	depends_on + declared Service → tier 1, 0.75. A compose service name is a local
//	                                alias the repository chose, so it earns less
//	                                than a DNS name even when it matches.
//
// The in-cluster Service name is tried before the Ingress host because it is the
// stronger claim.
func (consumeDeriver) match(consumed derive.HostFinding, services, ingress *hostIndex) (string, string, float64, bool) {
	fromComposeAlias := consumed.RuleID == derive.RuleConsumeComposeDependsOn

	if targetID, ok := services.resolve(derive.ServiceLabel(consumed.Host)); ok {
		if fromComposeAlias {
			return targetID, derive.RuleConsumeComposeDependsOn, derive.ConfidenceComposeTopology, true
		}
		return targetID, derive.RuleConsumeK8sServiceHost, derive.ConfidenceDeclaredHost, true
	}
	// A compose alias is never matched against a public hostname: the alias is a
	// container name on a private network, so an Ingress host it happened to equal
	// would be a coincidence rather than a reference.
	if !fromComposeAlias {
		if targetID, ok := ingress.resolve(consumed.Host); ok {
			return targetID, derive.RuleConsumeIngressHost, derive.ConfidenceComposeTopology, true
		}
	}
	return "", "", 0, false
}

// mergeConsumeEdge folds an edge into the agreement map, taking the max.
//
// Never sum and never multiply. Two rules reading the same docker-compose.yml
// share a root cause, so combining them as independent signals inflates the number
// — which is the whole reason confidence here is a fixed tier per rule and not a
// computed score.
func mergeConsumeEdge(agreement map[string]*DerivedEdge, edge DerivedEdge) {
	key := edge.SourceRepositoryID + "\x00" + edge.TargetRepositoryID + "\x00" + string(edge.Kind)
	existing, found := agreement[key]
	if !found {
		copied := edge
		agreement[key] = &copied
		return
	}
	if edge.Confidence > existing.Confidence {
		// The stronger rule owns the row's identity, so the fingerprint follows it
		// — otherwise the row would keep the weaker rule's identity forever.
		existing.Confidence = edge.Confidence
		existing.Fingerprint = edge.Fingerprint
		existing.Metadata["rule_id"] = edge.Metadata["rule_id"]
		existing.Metadata["tier"] = edge.Metadata["tier"]
	}
	// Both evidence paths are kept, because a person judging the edge wants to see
	// every file that produced it.
	existing.Metadata["also_evidence"] = appendUnique(
		stringsFromMetadata(existing.Metadata["also_evidence"]),
		stringOrEmpty(edge.Metadata["evidence_path"]))
	// Production evidence outranks test evidence for what the row *says* it is,
	// including its rule id — which is the field that makes "this came from a
	// mock" visible in the drawer. The fingerprint follows the rule id, because
	// the rule that owns the row has to own its identity too.
	if fromTest, _ := existing.Metadata["from_test_file"].(bool); fromTest {
		if edgeFromTest, _ := edge.Metadata["from_test_file"].(bool); !edgeFromTest {
			existing.Metadata["from_test_file"] = false
			existing.Metadata["evidence_path"] = edge.Metadata["evidence_path"]
			existing.Metadata["rule_id"] = edge.Metadata["rule_id"]
			existing.Metadata["tier"] = edge.Metadata["tier"]
			existing.Fingerprint = edge.Fingerprint
			if edge.Metadata["env_var_name"] != nil {
				existing.Metadata["env_var_name"] = edge.Metadata["env_var_name"]
			}
		}
	}
}

func stringsFromMetadata(value any) []string {
	list, _ := value.([]string)
	return list
}

func stringOrEmpty(value any) string {
	text, _ := value.(string)
	return text
}

// tierFor names the tier a rule belongs to, for the drawer.
func tierFor(ruleID string) string {
	switch ruleID {
	case derive.RuleConsumeComposeDependsOn:
		return "1"
	case derive.RuleConsumeK8sServiceHost:
		return "2a"
	case derive.RuleConsumeIngressHost:
		return "2b"
	default:
		return "unknown"
	}
}

// brokerVarMarkers are the variable-name fragments that mean a message broker
// rather than an HTTP endpoint.
//
// This is the one place a variable's *name* is consulted, and it decides only the
// edge's `kind` — never whether the edge exists. Getting it wrong mislabels an
// edge that a declaration already proved; it cannot invent one.
var brokerVarMarkers = []string{"KAFKA", "AMQP", "RABBIT", "BROKER", "NATS", "SQS", "PUBSUB", "TOPIC", "QUEUE"}

func isBrokerHost(consumed derive.HostFinding) bool {
	upper := strings.ToUpper(consumed.EnvVar)
	for _, marker := range brokerVarMarkers {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}
