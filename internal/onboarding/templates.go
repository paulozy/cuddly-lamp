// Package onboarding hosts the static registry of starter flows offered when an
// admin creates one.
//
// Templates are kept in code rather than in a table for the same reason the
// documentation templates are (`internal/docs/templates.go`): they evolve with
// the step kinds that render them, and a runtime store would let the two drift.
//
// A template seeds the *shape* of an onboarding, never its references. It
// cannot know which repository matters to a backend developer or who answers
// for access, so the referential steps it would love to include — repository,
// team, contacts — arrive as markdown notes telling the admin what to add. That
// keeps every stored step valid: no step is ever written in a state the
// renderer would have to apologise for.
package onboarding

import "github.com/paulozy/idp-with-ai-backend/internal/models"

// TemplateStep is one seeded step. Only kinds that are valid without a
// reference appear here — markdown, checklist, link, task, the org-wide views
// (architecture, glossary), and the team-membership verification.
//
// Config carries what those kinds need to be valid on arrival: a checklist
// needs items, a task needs instructions, a verification needs to name a check.
// A template that seeded a step failing validation would be a template nobody
// could save.
type TemplateStep struct {
	Kind             string                      `json:"kind"`
	Title            string                      `json:"title"`
	Body             string                      `json:"body,omitempty"`
	Config           models.OnboardingStepConfig `json:"config"`
	IsRequired       bool                        `json:"is_required"`
	EstimatedMinutes int                         `json:"estimated_minutes,omitempty"`
}

// Template is a starter flow.
type Template struct {
	ID          string         `json:"id"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Steps       []TemplateStep `json:"steps"`
}

// IDs are stable strings: they are accepted by the create endpoint and appear
// in the frontend's template picker. Do not change them after a release.
const (
	TemplateIDGeneral  = "general"
	TemplateIDBackend  = "backend-dev"
	TemplateIDFrontend = "frontend-dev"
)

// A note step is a placeholder the admin replaces with a real referential step.
// It is markdown so it renders as guidance rather than as a broken reference.
func note(title, body string) TemplateStep {
	return TemplateStep{Kind: "markdown", Title: title, Body: body, IsRequired: false, EstimatedMinutes: 2}
}

var templates = []Template{
	{
		ID:          TemplateIDGeneral,
		Label:       "Onboarding geral",
		Description: "Serve para qualquer pessoa que entra na organização, técnica ou não.",
		Steps: []TemplateStep{
			{
				Kind:             "markdown",
				Title:            "Bem-vindo",
				Body:             "# Bem-vindo\n\nEscreva aqui o que a organização faz, para quem, e o que muda com a chegada dessa pessoa.\n\nDuas ou três frases valem mais que uma página.",
				IsRequired:       true,
				EstimatedMinutes: 5,
			},
			{
				Kind:             "markdown",
				Title:            "Como trabalhamos",
				Body:             "## Como trabalhamos\n\nRituais, canais, horários combinados, o que é assíncrono e o que não é.\n\nSe existe um documento sobre isso, prefira um passo do tipo `doc` apontando para ele — assim não há duas versões.",
				IsRequired:       true,
				EstimatedMinutes: 10,
			},
			{
				Kind:             "glossary",
				Title:            "Vocabulário interno",
				Body:             "",
				IsRequired:       false,
				EstimatedMinutes: 5,
			},
			note(
				"Adicione as pessoas de referência",
				"Troque este passo por um do tipo **contatos**, indicando quem procurar para acesso, para dúvida de produto e para urgência de produção.",
			),
			{
				Kind:  "task",
				Title: "Primeira tarefa",
				Config: models.OnboardingStepConfig{
					Instructions: "Aponte o link para o board ou para a issue reservada a quem está chegando, e diga em uma frase o que se espera dessa primeira entrega.",
				},
				IsRequired:       true,
				EstimatedMinutes: 15,
			},
		},
	},
	{
		ID:          TemplateIDBackend,
		Label:       "Dev backend",
		Description: "Serviços, integrações e o caminho de uma mudança até produção.",
		Steps: []TemplateStep{
			{
				Kind:             "markdown",
				Title:            "Bem-vindo ao time de backend",
				Body:             "# Bem-vindo\n\nO que os serviços fazem, quais são os limites entre eles, e onde essa pessoa vai mexer primeiro.",
				IsRequired:       true,
				EstimatedMinutes: 5,
			},
			{
				Kind:             "architecture",
				Title:            "Como os serviços se relacionam",
				Body:             "",
				IsRequired:       true,
				EstimatedMinutes: 10,
			},
			note(
				"Apresente os serviços principais",
				"Troque este passo por um do tipo **repositório** para cada serviço que a pessoa vai tocar nas primeiras semanas. Cada um traz o stack, o scorecard e o time responsável, sempre atualizados.",
			),
			note(
				"Apresente o time responsável",
				"Troque este passo por um do tipo **time**, para a pessoa saber quem responde pelo quê antes de precisar perguntar.",
			),
			{
				Kind:  "checklist",
				Title: "Ambiente local",
				Config: models.OnboardingStepConfig{
					Items: []models.OnboardingStepChecklistItem{
						{Text: "Clonar o repositório principal"},
						{Text: "Instalar as dependências e rodar a suíte de testes"},
						{Text: "Subir o ambiente local e acessar a aplicação"},
						{Text: "Conseguir acesso aos serviços que o time usa"},
					},
				},
				IsRequired:       true,
				EstimatedMinutes: 30,
			},
			{
				Kind:             "glossary",
				Title:            "Vocabulário interno",
				Body:             "",
				IsRequired:       false,
				EstimatedMinutes: 5,
			},
			{
				// The check that needs no reference. The one worth adding by
				// hand is `first_change_request`, which has to name a
				// repository the template cannot know.
				Kind:             "verified",
				Title:            "Você já está num time",
				Config:           models.OnboardingStepConfig{Check: models.OnboardingCheckTeamMembership},
				IsRequired:       false,
				EstimatedMinutes: 1,
			},
			note(
				"Verifique a primeira contribuição",
				"Adicione um passo do tipo **verificado** com a checagem `first_change_request` apontando para o repositório onde essa pessoa vai abrir o primeiro PR ou MR. A plataforma confirma sozinha quando acontecer.",
			),
		},
	},
	{
		ID:          TemplateIDFrontend,
		Label:       "Dev frontend",
		Description: "Interface, design system e o contrato com a API.",
		Steps: []TemplateStep{
			{
				Kind:             "markdown",
				Title:            "Bem-vindo ao time de frontend",
				Body:             "# Bem-vindo\n\nQuais produtos existem, quem são os usuários, e o que está em construção agora.",
				IsRequired:       true,
				EstimatedMinutes: 5,
			},
			note(
				"Apresente o app principal",
				"Troque este passo por um do tipo **repositório** apontando para o front principal.",
			),
			{
				Kind:             "markdown",
				Title:            "Design system e convenções",
				Body:             "## Design system\n\nOnde vivem os tokens e os componentes, e o que se pode ou não criar por fora.\n\nSe houver documentação gerada, prefira um passo do tipo `doc`.",
				IsRequired:       true,
				EstimatedMinutes: 15,
			},
			{
				Kind:  "checklist",
				Title: "Ambiente local",
				Config: models.OnboardingStepConfig{
					Items: []models.OnboardingStepChecklistItem{
						{Text: "Clonar o repositório do front"},
						{Text: "Instalar as dependências e subir o dev server"},
						{Text: "Rodar os testes e o lint"},
					},
				},
				IsRequired:       true,
				EstimatedMinutes: 20,
			},
			{
				Kind:  "task",
				Title: "Primeira tarefa",
				Config: models.OnboardingStepConfig{
					Instructions: "Aponte o link para o board ou para a issue reservada a quem está chegando, e diga em uma frase o que se espera dessa primeira entrega.",
				},
				IsRequired:       true,
				EstimatedMinutes: 15,
			},
		},
	},
}

// Templates returns the registry, for the frontend's picker.
func Templates() []Template {
	out := make([]Template, len(templates))
	copy(out, templates)
	return out
}

// TemplateByID returns a template, or nil when the id is unknown.
func TemplateByID(id string) *Template {
	for i := range templates {
		if templates[i].ID == id {
			t := templates[i]
			return &t
		}
	}
	return nil
}
