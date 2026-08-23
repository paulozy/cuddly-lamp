package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/derive"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// resourcesExtractor reads a repository's committed configuration for runtime
// infrastructure and for the hostnames it declares and consumes.
//
// It writes two fact kinds from one pass over the same files. Splitting them into
// two extractors would double the request budget for no gain — a compose file
// holds both an engine image and an environment value naming another service.
type resourcesExtractor struct{}

// maxConfigFiles bounds one repository's configuration read.
//
// A Kustomize tree with overlays per environment can hold dozens of manifests, and
// most of them are patches of each other. Above the ceiling the facts are marked
// incomplete rather than silently truncated, for the same reason as everywhere
// else: a short read that looked complete would authorise a sweep.
const maxConfigFiles = 40

func (resourcesExtractor) Kind() models.RepositoryFactKind { return models.FactKindResources }

func (resourcesExtractor) Version() int { return 1 }

// isConfigCandidate is the union of every file the resource rules read.
func isConfigCandidate(filePath string) bool {
	return derive.IsComposeFile(filePath) ||
		derive.IsHelmChart(filePath) ||
		derive.IsHelmValues(filePath) ||
		derive.IsK8sManifest(filePath) ||
		derive.IsDotEnvExample(filePath)
}

func (resourcesExtractor) Extract(ctx context.Context, in ExtractInput) (any, derive.Outcome) {
	collector, outcome := collectConfig(ctx, in)
	return collector.ResourcesFact(), outcome
}

// hostsExtractor writes the `hosts` fact from the same collected configuration.
//
// It is a second Extractor over the same files rather than a field on the first
// because the two facts have independent lifecycles: `hosts` feeds an org-wide
// join (phase 4) while `resources` feeds per-repository nodes, and a fact kind is
// the unit the tree-SHA guard and the completeness gate operate on.
type hostsExtractor struct{}

func (hostsExtractor) Kind() models.RepositoryFactKind { return models.FactKindHosts }

func (hostsExtractor) Version() int { return 1 }

func (hostsExtractor) Extract(ctx context.Context, in ExtractInput) (any, derive.Outcome) {
	collector, outcome := collectConfig(ctx, in)
	return collector.HostsFact(), outcome
}

// collectConfig is the single read of a repository's configuration.
//
// Both extractors call it, so a repository's config is parsed twice per sync in
// exchange for keeping the two fact kinds independent. That is a deliberate trade:
// the parse is CPU on bytes already fetched, while the alternative — one fact kind
// carrying both payloads — would mean a truncated tree withdrawing the sweep for
// two unrelated things at once.
func collectConfig(ctx context.Context, in ExtractInput) (*derive.ResourceCollector, derive.Outcome) {
	outcome := derive.CompleteOutcome()
	if in.Truncated {
		outcome.MarkIncomplete(derive.ReasonTreeTruncated)
	}

	collector := derive.NewResourceCollector()
	candidates := in.Shortlist(isConfigCandidate)
	if len(candidates) > maxConfigFiles {
		outcome.MarkIncomplete(derive.ReasonTooManyCandidates)
		candidates = candidates[:maxConfigFiles]
	}

	for _, candidate := range candidates {
		content, err := in.Fetch(ctx, candidate)
		if err != nil {
			if !errors.Is(err, scm.ErrNotFound) {
				outcome.MarkIncomplete(derive.ReasonReadFailed)
				utils.Warn("architecture: config file unreadable",
					"repo_id", in.RepositoryID, "path", candidate, "error", err)
			}
			continue
		}
		if parseErr := readConfigFile(collector, candidate, content); parseErr != nil {
			// A templated Helm manifest legitimately fails to parse as YAML. It is
			// still an incompleteness — we do not know what it declares — but it
			// is common enough that it must not be an error, only a withdrawal of
			// the authority to delete.
			outcome.MarkIncomplete(derive.ReasonParseFailed)
			utils.Info("architecture: config file not parseable as committed",
				"repo_id", in.RepositoryID, "path", candidate, "error", parseErr)
		}
	}
	return collector, outcome
}

// readConfigFile dispatches one file to its reader and folds the result into the
// collector.
func readConfigFile(collector *derive.ResourceCollector, filePath string, content []byte) error {
	switch {
	case derive.IsComposeFile(filePath):
		return readCompose(collector, filePath, content)
	case derive.IsHelmChart(filePath):
		return readHelmChart(collector, filePath, content)
	case derive.IsK8sManifest(filePath):
		return readK8sManifest(collector, filePath, content)
	case derive.IsHelmValues(filePath):
		return readHelmValues(collector, filePath, content)
	case derive.IsDotEnvExample(filePath):
		// An example file has no host worth trusting, but it does say which
		// engines the application talks to.
		collector.ScanEnvironment(derive.ParseDotEnv(content), filePath, false)
		return nil
	default:
		return nil
	}
}

func readCompose(collector *derive.ResourceCollector, filePath string, content []byte) error {
	file, err := derive.ParseCompose(filePath, content)
	if err != nil {
		return err
	}

	engines := make(map[string]string, len(file.Services))
	for _, service := range file.Services {
		if service.Engine != "" {
			engines[service.Name] = service.Engine
			// A compose file describes a development environment, so the resource
			// belongs to THIS repository and is never unified with another's. Two
			// repositories each running `postgres:16` locally are two nodes,
			// because they are not the same Postgres.
			collector.AddResource(derive.ResourceFinding{
				Locator:     derive.Locator{Engine: service.Engine},
				Scoped:      true,
				DisplayName: service.Name + " (local)",
				RuleID:      derive.RuleResourceComposeImage,
				Confidence:  derive.ConfidenceLocalEvidence,
			}, filePath)
		}
		collector.ScanEnvironment(service.Environment, filePath, file.IsTestCompose)
	}

	for _, service := range file.Services {
		for _, target := range service.DependsOn {
			if target == service.Name {
				continue
			}
			collector.AddComposeDependency(derive.ComposeDependency{
				From:         service.Name,
				To:           target,
				ToEngine:     engines[target],
				EvidencePath: filePath,
				FromTestFile: file.IsTestCompose,
			})
			// A `depends_on` naming an engine is an edge to a Resource, which the
			// image rule above already recorded. Only a non-engine target is a
			// candidate for a Component edge — and even then only if another
			// repository declares a Service with that name.
			if engines[target] == "" {
				collector.AddConsumedService(target, filePath, file.IsTestCompose)
			}
		}
	}
	return nil
}

func readHelmChart(collector *derive.ResourceCollector, filePath string, content []byte) error {
	dependencies, err := derive.ParseHelmChart(filePath, content)
	if err != nil {
		return err
	}
	for _, dep := range dependencies {
		name := dep.Alias
		if name == "" {
			name = dep.Name
		}
		// A subchart is very strong evidence of the engine and none at all of the
		// instance, so it is scoped exactly like a compose image.
		collector.AddResource(derive.ResourceFinding{
			Locator:     derive.Locator{Engine: dep.Engine},
			Scoped:      true,
			DisplayName: name + " (subchart)",
			RuleID:      derive.RuleResourceHelmSubchart,
			Confidence:  derive.ConfidenceLocalEvidence,
		}, filePath)
	}
	return nil
}

func readK8sManifest(collector *derive.ResourceCollector, filePath string, content []byte) error {
	manifest, err := derive.ParseK8sManifest(filePath, content)
	// Partial results are kept even on error: a multi-document file whose third
	// document is a Helm template still told us about the first two.
	for _, name := range manifest.ServiceNames {
		collector.AddDeclaredService(name)
	}
	for _, host := range manifest.IngressHosts {
		collector.AddDeclaredHost(host)
	}
	collector.ScanEnvironment(manifest.Environment, filePath, false)
	return err
}

func readHelmValues(collector *derive.ResourceCollector, filePath string, content []byte) error {
	values, err := derive.FlattenYAMLStrings(content)
	if err != nil {
		return err
	}
	// values.yaml is plain YAML and parses, but its values may be defaults nobody
	// runs in production. It is read for hostnames and DSNs and never trusted on
	// its own to unify a resource — ScanEnvironment already refuses a placeholder,
	// and a default like `localhost` is exactly that.
	collector.ScanEnvironment(values, filePath, false)
	return nil
}

// ── the reconciler ───────────────────────────────────────────────────────────

const resourceDiscoveryVersion = "v1"

// resourceDerivationKey scopes the resource sweep.
//
// The key is per repository even though a *shared* resource row is org-wide,
// because what is being reconciled is the repository's claim to use it — the
// `repository_resources` join. The resource row itself is only ever created, never
// swept: another repository may still point at it, and deciding that requires the
// join, not this key.
func resourceDerivationKey(repositoryID string) string {
	return "resource:" + resourceDiscoveryVersion + ":repo/" + repositoryID
}

func (s *ArchitectureService) reconcileResources(ctx context.Context, organizationID string, fact models.RepositoryFact, runStartedAt time.Time) (ReconcileStats, error) {
	stats := ReconcileStats{}
	derivationKey := resourceDerivationKey(fact.RepositoryID)

	var payload derive.ResourcesFact
	outcome := UnmarshalFactPayload(&fact, &payload)

	suppressed, err := s.suppressedFingerprints(ctx, organizationID, derivationKey)
	if err != nil {
		return stats, err
	}

	seenAt := runStartedAt.Add(time.Millisecond)
	for _, finding := range payload.Resources {
		fingerprint := Fingerprint(finding.RuleID, finding.Locator.Engine,
			finding.Locator.Host, finding.Locator.Namespace, finding.DisplayName)
		if suppressed[fingerprint] {
			continue
		}

		resource := &models.Resource{
			OrganizationID: organizationID,
			Engine:         finding.Locator.Engine,
			Host:           finding.Locator.Host,
			Port:           finding.Locator.Port,
			Namespace:      finding.Locator.Namespace,
			DisplayName:    finding.DisplayName,
			Metadata: map[string]any{
				"rule_id":    finding.RuleID,
				"confidence": finding.Confidence,
				"evidence":   finding.Evidence,
			},
		}
		if finding.Scoped {
			// The scoped case is the honest one: the evidence named an engine and
			// not an instance, so this row belongs to this repository alone.
			repositoryID := fact.RepositoryID
			resource.ScopedRepositoryID = &repositoryID
		}
		key, print := derivationKey, fingerprint
		resource.DerivationKey = &key
		resource.DerivationFingerprint = &print
		resource.LastSeenAt = &seenAt

		if err := s.repo.UpsertDerivedResource(ctx, resource); err != nil {
			return stats, fmt.Errorf("upsert derived resource: %w", err)
		}

		link := &models.RepositoryResource{
			OrganizationID:        organizationID,
			RepositoryID:          fact.RepositoryID,
			ResourceID:            resource.ID,
			Confidence:            finding.Confidence,
			DerivationKey:         &key,
			DerivationFingerprint: &print,
			LastSeenAt:            &seenAt,
			Metadata: map[string]any{
				"rule_id":  finding.RuleID,
				"evidence": finding.Evidence,
			},
		}
		if err := s.repo.UpsertDerivedRepositoryResource(ctx, link); err != nil {
			return stats, fmt.Errorf("upsert repository resource: %w", err)
		}
		stats.Upserted++
	}

	if !outcome.Complete {
		stats.SweepSkipped = true
		utils.Info("architecture: skipping resource sweep, fact incomplete",
			"repo_id", fact.RepositoryID, "reasons", strings.Join(outcome.Reasons, ","))
		return stats, nil
	}

	swept, err := s.repo.SweepDerivedRepositoryResources(ctx, fact.RepositoryID, derivationKey, runStartedAt)
	if err != nil {
		return stats, fmt.Errorf("sweep repository resources: %w", err)
	}
	stats.Swept = swept
	return stats, nil
}
