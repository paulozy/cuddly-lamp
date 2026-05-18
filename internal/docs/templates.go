// Package docs hosts the static registry of documentation templates exposed
// to the frontend's template-gallery modal.
//
// Templates are intentionally kept in code (not a database table) because
// they evolve with the prompt logic in `internal/integrations/anthropic`;
// a runtime store would let the two drift out of sync.
package docs

// TemplateScope mirrors models.DocGenerationScope but is duplicated here so
// this package stays a leaf with no dependency on the models package.
type TemplateScope string

const (
	TemplateScopeOrg TemplateScope = "org"
)

// TemplateType groups templates by the kind of document they produce.
type TemplateType string

const (
	TemplateTypeADR           TemplateType = "adr"
	TemplateTypeArchitecture  TemplateType = "architecture"
	TemplateTypeGuidelines    TemplateType = "guidelines"
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
)

// Templates returns the immutable list of templates exposed to the UI.
// Order matters: the gallery renders them in this sequence.
func Templates() []DocTemplate {
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

// GetTemplate returns the template by ID and whether it was found.
func GetTemplate(id string) (DocTemplate, bool) {
	for _, t := range Templates() {
		if t.ID == id {
			return t, true
		}
	}
	return DocTemplate{}, false
}
