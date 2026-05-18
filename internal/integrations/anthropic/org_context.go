package anthropic

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

// OrgContextBuilder aggregates the org-wide snapshot fed to Claude when
// generating org-level docs (ADRs / Architecture / Guidelines).
//
// The output is a single Markdown blob deliberately structured as: header
// (org metadata, dominant stacks) → repositories table → relationships list
// → titles of existing per-repo docs. For a 50-repo org the total runs
// around 30-50K tokens, well inside the Anthropic context window.
type OrgContextBuilder struct {
	repo storage.Repository
}

func NewOrgContextBuilder(repo storage.Repository) *OrgContextBuilder {
	return &OrgContextBuilder{repo: repo}
}

// OrgContextSnapshot is the structured output of the builder. The Markdown
// is the value Claude consumes; the other fields are exposed for tests and
// potential UI surfacing.
type OrgContextSnapshot struct {
	OrganizationID   string
	OrganizationName string
	RepositoryCount  int
	DominantStacks   []string
	Markdown         string
}

// Build assembles the org-wide context for the given organization.
//
// Returns (nil, nil) when the org has no repositories — the worker treats
// that as a graceful skip (no point asking Claude to write an ADR for an
// empty org).
func (b *OrgContextBuilder) Build(ctx context.Context, orgID string) (*OrgContextSnapshot, error) {
	org, err := b.repo.GetOrganization(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("org context: get organization: %w", err)
	}
	if org == nil {
		return nil, fmt.Errorf("org context: organization not found: %s", orgID)
	}

	repos, _, err := b.repo.ListRepositories(ctx, &storage.RepositoryFilter{
		OrganizationID: orgID,
		Limit:          200, // hard cap to keep prompt size bounded
	})
	if err != nil {
		return nil, fmt.Errorf("org context: list repositories: %w", err)
	}
	if len(repos) == 0 {
		return nil, nil
	}

	rels, err := b.repo.ListRepositoryRelationships(ctx, storage.RepositoryRelationshipFilter{
		OrganizationID: orgID,
	})
	if err != nil {
		return nil, fmt.Errorf("org context: list relationships: %w", err)
	}

	// Tally dominant stacks (languages + frameworks) across all repos so the
	// header line surfaces what the org is "actually built in" — useful
	// signal for the model when proposing technology choices.
	// Metadata.Languages is map[name]bytes (rough size from GitHub linguist).
	// For aggregation we only need presence, so count one per repo per key.
	langCount := map[string]int{}
	fwCount := map[string]int{}
	for i := range repos {
		for lang := range repos[i].Metadata.Languages {
			if lang = strings.TrimSpace(lang); lang != "" {
				langCount[lang]++
			}
		}
		for _, fw := range repos[i].Metadata.Frameworks {
			if fw = strings.TrimSpace(fw); fw != "" {
				fwCount[fw]++
			}
		}
	}

	snapshot := &OrgContextSnapshot{
		OrganizationID:   org.ID,
		OrganizationName: org.Name,
		RepositoryCount:  len(repos),
		DominantStacks:   append(topNByCount(langCount, 5), topNByCount(fwCount, 5)...),
	}

	var b2 strings.Builder
	fmt.Fprintf(&b2, "# Organization: %s\n\n", org.Name)
	fmt.Fprintf(&b2, "**Repositories**: %d\n", len(repos))
	if langs := topNByCount(langCount, 5); len(langs) > 0 {
		fmt.Fprintf(&b2, "**Top languages**: %s\n", strings.Join(langs, ", "))
	}
	if fws := topNByCount(fwCount, 5); len(fws) > 0 {
		fmt.Fprintf(&b2, "**Top frameworks**: %s\n", strings.Join(fws, ", "))
	}
	b2.WriteString("\n## Repositories\n\n")
	b2.WriteString("| Name | Provider | Languages | Frameworks | Analysis |\n")
	b2.WriteString("| --- | --- | --- | --- | --- |\n")
	for i := range repos {
		r := &repos[i]
		langs := joinTrimmed(mapKeys(r.Metadata.Languages), ", ", 60)
		fws := joinTrimmed(r.Metadata.Frameworks, ", ", 60)
		analysisLine := "—"
		if a, _ := b.repo.GetLatestAnalysis(ctx, r.ID, models.AnalysisTypeCodeReview); a != nil && a.SummaryText != "" {
			analysisLine = truncate(strings.ReplaceAll(a.SummaryText, "\n", " "), 120)
		}
		fmt.Fprintf(&b2, "| %s | %s | %s | %s | %s |\n", r.Name, r.Type, dashIfEmpty(langs), dashIfEmpty(fws), analysisLine)
	}

	if len(rels) > 0 {
		b2.WriteString("\n## Relationships\n\n")
		repoNames := map[string]string{}
		for i := range repos {
			repoNames[repos[i].ID] = repos[i].Name
		}
		for i := range rels {
			rel := &rels[i]
			src := nameOrID(repoNames, rel.SourceRepositoryID)
			dst := nameOrID(repoNames, rel.TargetRepositoryID)
			fmt.Fprintf(&b2, "- %s → %s (`%s`)\n", src, dst, rel.Kind)
		}
	}

	// Titles of existing per-repo docs help Claude reference prior decisions
	// without including the (much larger) Markdown bodies.
	docTitles := map[string][]string{}
	for i := range repos {
		docs, err := b.repo.ListDocGenerationsForRepo(ctx, repos[i].ID)
		if err != nil {
			continue
		}
		for _, doc := range docs {
			if doc.Status != models.DocGenerationStatusCompleted {
				continue
			}
			for _, t := range doc.Types {
				docTitles[repos[i].Name] = appendUnique(docTitles[repos[i].Name], string(t))
			}
		}
	}
	if len(docTitles) > 0 {
		b2.WriteString("\n## Existing Per-Repo Documentation\n\n")
		names := make([]string, 0, len(docTitles))
		for n := range docTitles {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(&b2, "- %s: %s\n", name, strings.Join(docTitles[name], ", "))
		}
	}

	snapshot.Markdown = b2.String()
	return snapshot, nil
}

func topNByCount(counts map[string]int, n int) []string {
	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(counts))
	for k, v := range counts {
		if k == "" {
			continue
		}
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})
	out := make([]string, 0, n)
	for i := 0; i < len(pairs) && i < n; i++ {
		out = append(out, pairs[i].k)
	}
	return out
}

func joinTrimmed(parts []string, sep string, limit int) string {
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			cleaned = append(cleaned, p)
		}
	}
	return truncate(strings.Join(cleaned, sep), limit)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func nameOrID(m map[string]string, id string) string {
	if name, ok := m[id]; ok {
		return name
	}
	return id
}

func appendUnique(slice []string, value string) []string {
	for _, s := range slice {
		if s == value {
			return slice
		}
	}
	return append(slice, value)
}

// mapKeys returns the keys of a map[string]int as a slice in sorted order.
// Sorted output keeps the prompt deterministic across builds (same map iter
// order is otherwise undefined in Go).
func mapKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
