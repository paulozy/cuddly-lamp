// Package derive turns a repository's committed files into architecture facts.
//
// It mirrors internal/detect on purpose: extraction is I/O and derivation is a
// pure function. The caller reads the tree and the file bytes and hands them in,
// which makes every rule testable against literals and keeps the heuristics in
// one auditable place.
//
// The governing asymmetry is detect's, sharpened: a wrong edge asserted at high
// confidence is worse than no edge, because a catalog nobody trusts costs
// maintenance and returns no decision. So a rule that cannot tell two answers
// apart produces neither.
//
// Confidence is a fixed tier per rule, never a computed number. There is no
// established numeric scheme in the industry — what exists is ordinal
// provenance (deps.dev's SLSA_ATTESTATION … UNVERIFIED_METADATA enum is the
// reference). Two rules reading the same docker-compose.yml are not
// independent, so agreement takes the max and records both pieces of evidence.
// Multiplying or averaging correlated signals inflates the number.
package derive

// Confidence tiers. Fixed per rule; see the package comment for why these are
// constants and not a formula.
const (
	// ConfidenceExactName is an exact match of an internal package name against
	// the organization's own index of published names. Nothing is guessed.
	ConfidenceExactName = 1.00
	// ConfidenceDeclaredHost is a hostname in a config value matching a
	// Kubernetes Service another repository declares. The match is against a
	// declaration, not a naming convention.
	ConfidenceDeclaredHost = 0.85
	// ConfidenceSharedLocator is the same (engine, host, port, namespace) found
	// in two repositories' config.
	ConfidenceSharedLocator = 0.85
	// ConfidenceComposeTopology is a compose file's own topology: high
	// precision about the environment it describes, which is usually dev.
	ConfidenceComposeTopology = 0.75
	// ConfidenceLocalEvidence is a compose image or a Helm subchart. It proves
	// the repository uses that engine and nothing about which instance.
	ConfidenceLocalEvidence = 0.70
	// ConfidenceExtensionSniff is a file whose kind was inferred from its
	// extension plus a content probe, because the format has no root marker —
	// GraphQL SDL and protobuf.
	ConfidenceExtensionSniff = 0.60
)

// maxEvidence caps how many paths a fact records, so a row stays small. The
// point of evidence is ending an argument, and three paths do that.
const maxEvidence = 3

// Evidence is the proof behind a derived fact: the paths that produced it,
// capped, plus the rule that read them.
//
// A derived edge without evidence is a support ticket. Naming the file lets a
// person judge in two seconds whether the deriver is right — which for the
// consumption edges of phase 4 is the only available defence against a
// repository that mocks its dependency in tests.
type Evidence struct {
	// RuleID identifies which rule fired. Stable string, never renamed, so a
	// bad result traces back to the rule that produced it.
	RuleID string   `json:"rule_id"`
	Paths  []string `json:"paths,omitempty"`
}

// AddPath records one more piece of proof, up to maxEvidence.
func (e *Evidence) AddPath(path string) {
	if len(e.Paths) >= maxEvidence {
		return
	}
	for _, existing := range e.Paths {
		if existing == path {
			return
		}
	}
	e.Paths = append(e.Paths, path)
}

// Incompleteness reasons, recorded on an Outcome so the UI can say something
// more useful than "we don't know".
const (
	// ReasonTreeTruncated is the provider's listing running out before the tree
	// did. A path missing from a truncated listing proves nothing.
	ReasonTreeTruncated = "tree_truncated"
	// ReasonReadFailed is a file the provider would not serve for a reason other
	// than absence: a rate limit, a 5xx, a permission refusal.
	ReasonReadFailed = "read_failed"
	// ReasonParseFailed is a file that was read but could not be understood.
	ReasonParseFailed = "parse_failed"
	// ReasonTooManyCandidates is a shortlist above the per-repository ceiling.
	// Fetching all of them would cost more than the answer is worth.
	ReasonTooManyCandidates = "too_many_candidates"
)

// Outcome is what every extractor returns alongside its payload.
//
// Complete is the field that authorises deletion. It starts false and is set
// true only by an extractor that saw everything it needed to see, because the
// failure mode being defended against is a 429 looking exactly like "the
// dependency was removed" — and a sweep that cannot tell them apart deletes
// correct edges on a bad afternoon.
type Outcome struct {
	Complete bool `json:"complete"`
	// Reasons names why Complete is false, deduplicated. Empty when complete.
	Reasons []string `json:"reasons,omitempty"`
}

// CompleteOutcome is the starting point for an extractor that has seen a full
// listing. Any failure along the way calls MarkIncomplete.
func CompleteOutcome() Outcome {
	return Outcome{Complete: true}
}

// MarkIncomplete records a reason and withdraws the authority to sweep.
func (o *Outcome) MarkIncomplete(reason string) {
	o.Complete = false
	for _, existing := range o.Reasons {
		if existing == reason {
			return
		}
	}
	o.Reasons = append(o.Reasons, reason)
}
