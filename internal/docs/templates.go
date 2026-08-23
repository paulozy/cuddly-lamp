// Package docs hosts the static registry of documentation templates exposed
// to the frontend's template-gallery modal.
//
// Templates are intentionally kept in code (not a database table) because
// they evolve with the prompt logic in `internal/integrations/anthropic`;
// a runtime store would let the two drift out of sync.
//
// The registry covers both scopes, and the difference between them is not
// cosmetic. An org template is picked one at a time and produces a document
// that lives only in this platform. A repo template is one of several a caller
// may request at once, and produces a *file in the repository* — which is why
// repo entries carry OutputPath and org entries cannot.
package docs

// TemplateScope mirrors models.DocGenerationScope but is duplicated here so
// this package stays a leaf with no dependency on the models package.
type TemplateScope string

const (
	TemplateScopeOrg  TemplateScope = "org"
	TemplateScopeRepo TemplateScope = "repo"
)

// TemplateType groups templates by the kind of document they produce. The
// values match ai.DocumentationType, which is what the worker dispatches on.
type TemplateType string

const (
	TemplateTypeADR          TemplateType = "adr"
	TemplateTypeArchitecture TemplateType = "architecture"
	TemplateTypeGuidelines   TemplateType = "guidelines"
	// TemplateTypeServiceDoc exists only at repo scope — a service document
	// describes one service, so there is nothing for it to mean org-wide.
	TemplateTypeServiceDoc TemplateType = "service_doc"
)

// DocTemplate describes one template entry. The frontend renders this as a
// card in the template gallery: `Label` becomes the card title, `Description`
// the subtitle, and `Sections` the right-side preview.
type DocTemplate struct {
	ID          string        `json:"id"`
	Label       string        `json:"label"`
	Description string        `json:"description"`
	Type        TemplateType  `json:"type"`
	Scope       TemplateScope `json:"scope"`
	Sections    []string      `json:"sections"`
	// OutputPath is the file the generated doc lands on inside the repository.
	// Empty for org-scope templates, which have no repository to commit to.
	//
	// This is the most concrete answer to "what comes out of this?" — a file,
	// at this path, delivered as a pull request. It is also the single source
	// for the worker's type-to-path mapping; see PathForType.
	OutputPath string `json:"output_path,omitempty"`
}

// IDs are stable strings that the worker uses to pick a prompt. Do not change
// them after a release — they are persisted on `doc_generations.template_id`.
const (
	TemplateIDADRTechChoice  = "adr-tech-choice"
	TemplateIDADRBoundary    = "adr-service-boundary"
	TemplateIDADRDeprecation = "adr-deprecation"
	TemplateIDADRConvention  = "adr-convention"
	TemplateIDArchitecture   = "architecture-overview"
	TemplateIDGuidelines     = "guidelines-engineering"

	// Repo-scope IDs. These are not selectable per-template the way org ones
	// are — the repo flow picks types, not templates — but they keep the
	// registry uniform and give the UI a stable React key.
	TemplateIDRepoADR          = "repo-adr"
	TemplateIDRepoArchitecture = "repo-architecture"
	TemplateIDRepoServiceDoc   = "repo-service-doc"
	TemplateIDRepoGuidelines   = "repo-guidelines"
)

// Templates returns the immutable list of templates exposed to the UI.
// Order matters: the gallery renders them in this sequence.
//
// Prefer TemplatesForScope at call sites. Returning everything here is kept
// for the unscoped API response, which must not change shape.
func Templates() []DocTemplate {
	return append(orgTemplates(), repoTemplates()...)
}

// TemplatesForScope narrows the registry to one scope.
//
// Callers must pass a scope. Mixing the two in one gallery would offer an
// org-wide "engineering guidelines" next to a repository's CONTRIBUTING.md as
// if they were alternatives, which they are not.
func TemplatesForScope(scope TemplateScope) []DocTemplate {
	switch scope {
	case TemplateScopeOrg:
		return orgTemplates()
	case TemplateScopeRepo:
		return repoTemplates()
	default:
		return nil
	}
}

func orgTemplates() []DocTemplate {
	return []DocTemplate{
		{
			ID:          TemplateIDADRTechChoice,
			Label:       "Escolha de tecnologia",
			Description: "Decisão entre opções concorrentes (ex.: Postgres vs Mongo, REST vs gRPC). Formato MADR completo com alternativas avaliadas.",
			Type:        TemplateTypeADR,
			Scope:       TemplateScopeOrg,
			Sections:    []string{"Status", "Contexto", "Drivers da decisão", "Opções consideradas", "Decisão", "Prós e contras", "Consequências"},
		},
		{
			ID:          TemplateIDADRBoundary,
			Label:       "Boundary entre serviços",
			Description: "Decisões sobre como serviços se comunicam ou compartilham código (HTTP vs evento, lib compartilhada vs duplicação).",
			Type:        TemplateTypeADR,
			Scope:       TemplateScopeOrg,
			Sections:    []string{"Status", "Contexto", "Decisão", "Relacionamentos impactados", "Consequências"},
		},
		{
			ID:          TemplateIDADRDeprecation,
			Label:       "Política de deprecation",
			Description: "Como deprecar um endpoint, lib ou serviço. Inclui timeline e plano de migração.",
			Type:        TemplateTypeADR,
			Scope:       TemplateScopeOrg,
			Sections:    []string{"Status", "Contexto", "Decisão", "Timeline", "Plano de migração", "Consequências"},
		},
		{
			ID:          TemplateIDADRConvention,
			Label:       "Convenção transversal",
			Description: "Padrões cross-cutting da org (formato de log, padrão de auth, versionamento de API). Curto e prescritivo.",
			Type:        TemplateTypeADR,
			Scope:       TemplateScopeOrg,
			Sections:    []string{"No contexto de…", "Decidimos por…", "Para alcançar…", "Aceitando…"},
		},
		{
			ID:          TemplateIDArchitecture,
			Label:       "Visão geral da arquitetura",
			Description: "Documento C4 System Context da organização: stacks dominantes, serviços-chave, integrações, padrões transversais.",
			Type:        TemplateTypeArchitecture,
			Scope:       TemplateScopeOrg,
			Sections:    []string{"Visão geral", "Repositórios e responsabilidades", "Integrações principais", "Padrões transversais", "Próximos marcos"},
		},
		{
			ID:          TemplateIDGuidelines,
			Label:       "Diretrizes de engenharia",
			Description: "Engineering Guidelines da org: processo de PR, code style geral, naming, segurança, testes, observabilidade.",
			Type:        TemplateTypeGuidelines,
			Scope:       TemplateScopeOrg,
			Sections:    []string{"Processo de PR", "Code style", "Convenções de nomenclatura", "Segurança", "Testes", "Observabilidade"},
		},
	}
}

// repoTemplates describes what a repository-scope generation actually produces.
//
// Descriptions and sections are written to match the prompts in
// `internal/integrations/anthropic/documentation.go` — if a prompt changes,
// these change with it. Saying "Contexto, Decisão, Consequências" when the
// prompt asks for something else would be worse than saying nothing.
func repoTemplates() []DocTemplate {
	return []DocTemplate{
		{
			ID:          TemplateIDRepoArchitecture,
			Label:       "Arquitetura",
			Description: "Como este repositório é construído: um diagrama Mermaid dos módulos e suas relações, seguido do que o sistema faz, como as partes conversam e as decisões técnicas centrais.",
			Type:        TemplateTypeArchitecture,
			Scope:       TemplateScopeRepo,
			Sections:    []string{"Diagrama de componentes (Mermaid)", "O que o sistema faz", "Como os componentes interagem", "Decisões técnicas principais"},
			OutputPath:  "docs/ARCHITECTURE.md",
		},
		{
			ID:          TemplateIDRepoServiceDoc,
			Label:       "Serviço",
			Description: "O manual de operação: como rodar local e no Docker, variáveis de ambiente, endpoints detectados, como rodar os testes e o que se sabe que está quebrado.",
			Type:        TemplateTypeServiceDoc,
			Scope:       TemplateScopeRepo,
			Sections:    []string{"Visão geral", "Pré-requisitos", "Variáveis de ambiente", "Como rodar (local e Docker)", "Endpoints da API", "Rodando os testes", "Dependências principais", "Problemas conhecidos"},
			OutputPath:  "docs/SERVICE.md",
		},
		{
			ID:          TemplateIDRepoADR,
			Label:       "ADRs",
			Description: "De 2 a 5 Architecture Decision Records inferidos do código e do histórico de commits — escolhas de tecnologia, padrões adotados e trade-offs visíveis. Saem como Propostos, para você revisar.",
			Type:        TemplateTypeADR,
			Scope:       TemplateScopeRepo,
			Sections:    []string{"Título", "Data", "Status (Proposto)", "Contexto", "Decisão", "Consequências"},
			OutputPath:  "docs/adr/README.md",
		},
		{
			ID:          TemplateIDRepoGuidelines,
			Label:       "Diretrizes",
			Description: "Um CONTRIBUTING.md com o estilo de código inferido do próprio repositório, nomenclatura de branch, processo de PR, formato de commit e checklist de review.",
			Type:        TemplateTypeGuidelines,
			Scope:       TemplateScopeRepo,
			Sections:    []string{"Estilo de código", "Nomenclatura de branches", "Processo de PR", "Formato de commit", "Requisitos de teste", "Checklist de review"},
			// Not under docs/: this is the file GitHub and GitLab both look for
			// at the repository root to surface on the new-pull-request screen.
			OutputPath: "CONTRIBUTING.md",
		},
	}
}

// GetTemplate returns the template by ID and whether it was found.
func GetTemplate(id string) (DocTemplate, bool) {
	for _, t := range Templates() {
		if t.ID == id {
			return t, true
		}
	}
	return DocTemplate{}, false
}

// PathForType reports the repository file a generated document of this type
// lands on.
//
// This is the registry's answer to a question the worker used to answer for
// itself with a duplicate switch. Keeping the mapping next to the description
// that promises it is the point: the modal tells the user "this produces
// docs/ARCHITECTURE.md", and this is what makes that true.
func PathForType(docType string) (string, bool) {
	for _, t := range repoTemplates() {
		if string(t.Type) == docType {
			return t.OutputPath, true
		}
	}
	return "", false
}
