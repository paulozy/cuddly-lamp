package services

import (
	"context"
	"errors"
	"strings"

	"github.com/paulozy/idp-with-ai-backend/internal/derive"
	"github.com/paulozy/idp-with-ai-backend/internal/derive/ecosystems"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// packagesExtractor reads every manifest in a repository's tree.
//
// It is the I/O half of the library-dependency derivation: the shallow readers in
// internal/derive/ecosystems are pure, and this is the only part that touches the
// network. Which is why the whole rule set is testable against literals and this
// file has almost no logic of its own.
type packagesExtractor struct{}

// maxManifestsPerRepo bounds one repository's extraction.
//
// A monorepo with hundreds of package.json files is real, and reading all of
// them would turn one sync into hundreds of sequential requests. Above the
// ceiling the fact is marked incomplete with `too_many_candidates` rather than
// truncated silently — because a silently truncated read would look complete and
// authorise a sweep that deletes the edges of every manifest past the cut.
const maxManifestsPerRepo = 60

func (packagesExtractor) Kind() models.RepositoryFactKind { return models.FactKindPackages }

func (packagesExtractor) Version() int { return 1 }

func (packagesExtractor) Extract(ctx context.Context, in ExtractInput) (any, derive.Outcome) {
	outcome := derive.CompleteOutcome()
	// A truncated listing proves presence and never absence, so a manifest we
	// never saw is indistinguishable from one that was deleted.
	if in.Truncated {
		outcome.MarkIncomplete(derive.ReasonTreeTruncated)
	}

	candidates := in.Shortlist(ecosystems.IsManifest)
	if len(candidates) > maxManifestsPerRepo {
		outcome.MarkIncomplete(derive.ReasonTooManyCandidates)
		candidates = candidates[:maxManifestsPerRepo]
	}

	manifests := make([]ecosystems.Manifest, 0, len(candidates))
	for _, candidate := range candidates {
		content, err := in.Fetch(ctx, candidate)
		if err != nil {
			// Absence keeps the fact complete: the tree said the path exists and
			// the host now says it does not, which for a manifest listing means
			// a race with a commit, not a failure to inspect. Anything else —
			// a 429, a 5xx, a permission refusal — is a failure to inspect, and
			// the fact must lose its authority to delete.
			if !errors.Is(err, scm.ErrNotFound) {
				outcome.MarkIncomplete(derive.ReasonReadFailed)
				utils.Warn("architecture: manifest unreadable",
					"repo_id", in.RepositoryID, "path", candidate, "error", err)
			}
			continue
		}
		parser := ecosystems.ParserFor(candidate)
		if parser == nil {
			continue
		}
		manifest, err := parser(candidate, content)
		if err != nil {
			// A malformed manifest is this repository's problem, not ours, but we
			// genuinely do not know what it declares — so the fact is incomplete.
			outcome.MarkIncomplete(derive.ReasonParseFailed)
			utils.Warn("architecture: manifest unparseable",
				"repo_id", in.RepositoryID, "path", candidate, "error", err)
			continue
		}
		manifests = append(manifests, manifest)
	}

	return derive.PackagesFromManifests(manifests), outcome
}

// ── the deriver ──────────────────────────────────────────────────────────────

// libDepDeriver turns `packages` facts into repo→repo library edges.
//
// libDepVersion is the version segment of the derivation key. Bumping it re-keys
// every edge this deriver owns: the new key's rows are written fresh and the old
// key's rows are retired by their own sweep on the next run. That is the
// mechanism for changing matching logic *visibly* instead of having behaviour
// shift under the same key.
type libDepDeriver struct{}

const libDepVersion = "v1"

func (libDepDeriver) Key(organizationID string) string {
	return "libdep:" + libDepVersion + ":org/" + organizationID
}

func (libDepDeriver) FactKind() models.RepositoryFactKind { return models.FactKindPackages }

// Derive is a pure function over stored facts: no provider I/O, no database
// reads beyond what it was handed.
//
// The algorithm is deliberately small, because the identity work was already
// done by the extractors:
//
//  1. index every published name against the repository that publishes it
//  2. for every declared dependency, look it up
//     — hit, single owner  → internal edge, confidence 1.00
//     — hit, many owners   → ambiguous, no edge, recorded
//     — miss               → external dependency, nothing
//
// Nothing is guessed at any step.
func (d libDepDeriver) Derive(_ context.Context, _ string, facts []models.RepositoryFact) (DerivedSet, error) {
	set := DerivedSet{Complete: true}

	index := derive.NewPackageIndex()
	payloads := make(map[string]derive.PackagesFact, len(facts))

	for i := range facts {
		var payload derive.PackagesFact
		outcome := UnmarshalFactPayload(&facts[i], &payload)
		if !outcome.Complete {
			// One repository's incomplete extraction withdraws the sweep for the
			// *whole* organization, and that is correct rather than harsh: the
			// index is org-wide, so a name this repository failed to publish
			// hides an edge from every other repository to it. Sweeping then
			// deletes correct edges that were merely unobservable this run.
			set.Complete = false
			for _, reason := range outcome.Reasons {
				set.Reasons = appendUnique(set.Reasons, reason)
			}
		}
		payloads[facts[i].RepositoryID] = payload
		for _, published := range payload.Published {
			index.Add(published.Ecosystem, published.Name, facts[i].RepositoryID)
		}
	}

	// A contested name yields no edge at all. Recording it lets the UI ask a
	// person to resolve it, which is the only correct answer available.
	for _, ambiguity := range index.Ambiguities() {
		utils.Info("architecture: package name claimed by more than one repository",
			"ecosystem", ambiguity.Ecosystem, "package", ambiguity.Name, "claimants", strings.Join(ambiguity.Claimants, ","))
	}

	for repositoryID, payload := range payloads {
		seen := make(map[string]bool, len(payload.Requires))
		for _, required := range payload.Requires {
			ownerID, ambiguous, found := index.Resolve(required.Ecosystem, required.Name)
			if !found || ambiguous {
				continue
			}
			// A monorepo commonly declares a dependency on a package it
			// publishes itself. That is not an edge, and filtering it here keeps
			// it away from the no_self_repository_relationship CHECK.
			if ownerID == repositoryID {
				continue
			}

			ruleID := derive.RuleForEcosystem(required.Ecosystem)
			// The manifest path is part of the identity so a monorepo declaring
			// the same dependency from two of its manifests yields two edges
			// with two pieces of evidence, rather than one row that flickers
			// between them. The version deliberately is not.
			fingerprint := Fingerprint(ruleID, required.Ecosystem, required.Name, required.ManifestPath)
			if seen[fingerprint] {
				continue
			}
			seen[fingerprint] = true

			label := required.RawName
			if label == "" {
				label = required.Name
			}
			set.Edges = append(set.Edges, DerivedEdge{
				SourceRepositoryID: repositoryID,
				TargetRepositoryID: ownerID,
				Kind:               models.RepositoryRelationshipKindLibrary,
				Source:             models.RepositoryRelationshipSourceManifest,
				Confidence:         derive.ConfidenceExactName,
				Label:              label,
				Fingerprint:        fingerprint,
				Metadata: map[string]any{
					"rule_id":          ruleID,
					"ecosystem":        required.Ecosystem,
					"package_name":     required.Name,
					"manifest_path":    required.ManifestPath,
					"purl":             required.PURL,
					"declared_version": required.Version,
				},
			})
		}
	}
	return set, nil
}

func appendUnique(list []string, value string) []string {
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}
