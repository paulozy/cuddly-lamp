package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// OnboardingVerifier runs the checks a `verified` step can actually prove.
//
// It exists as its own type because it is the only part of onboarding that
// talks to a provider: keeping it separate leaves the rest of the service
// dependent on storage alone, and lets a deployment without provider
// credentials still serve every other kind of step.
//
// Every result says how it was checked. A verification that cannot explain
// itself is indistinguishable from a checkbox, and the whole reason this exists
// is to be more than one.
type OnboardingVerifier struct {
	repo    storage.Repository
	resolve scm.ResolverFunc
	// hosts carries the deployment's provider API roots, with no tokens.
	hosts scm.Credentials
}

func NewOnboardingVerifier(repo storage.Repository, hosts scm.Credentials) *OnboardingVerifier {
	return &OnboardingVerifier{repo: repo, resolve: scm.For, hosts: hosts}
}

// Verify runs the step's check for one person.
func (v *OnboardingVerifier) Verify(ctx context.Context, orgID, userID string, step *models.OnboardingStep) (models.OnboardingVerificationResult, error) {
	cfg, err := step.DecodeConfig()
	if err != nil {
		return models.OnboardingVerificationResult{}, err
	}

	switch cfg.Check {
	case models.OnboardingCheckTeamMembership:
		return v.verifyTeamMembership(ctx, orgID, userID, cfg.TeamID)
	case models.OnboardingCheckFirstChangeRequest:
		return v.verifyFirstChangeRequest(ctx, orgID, userID, cfg.RepositoryID)
	default:
		return models.OnboardingVerificationResult{
			Pending: true,
			How:     fmt.Sprintf("Verificação %q não é reconhecida por esta versão.", cfg.Check),
		}, nil
	}
}

// verifyTeamMembership answers from local data, so it is always conclusive.
func (v *OnboardingVerifier) verifyTeamMembership(ctx context.Context, orgID, userID, teamID string) (models.OnboardingVerificationResult, error) {
	teamIDs, err := v.repo.ListTeamIDsForUser(ctx, orgID, userID)
	if err != nil {
		return models.OnboardingVerificationResult{}, err
	}

	if teamID == "" {
		if len(teamIDs) > 0 {
			return models.OnboardingVerificationResult{
				Passed: true,
				How:    fmt.Sprintf("Você está em %d time(s) desta organização.", len(teamIDs)),
			}, nil
		}
		return models.OnboardingVerificationResult{
			How: "Você ainda não está em nenhum time. Peça para alguém do time te adicionar.",
		}, nil
	}

	team, err := v.repo.GetTeam(ctx, teamID)
	if err != nil {
		return models.OnboardingVerificationResult{}, err
	}
	if team == nil || team.OrganizationID != orgID {
		return models.OnboardingVerificationResult{
			Pending: true,
			How:     "O time que este passo verifica não existe mais.",
		}, nil
	}

	for _, id := range teamIDs {
		if id == teamID {
			return models.OnboardingVerificationResult{
				Passed: true,
				How:    fmt.Sprintf("Você está no time %s.", team.Name),
			}, nil
		}
	}
	return models.OnboardingVerificationResult{
		How: fmt.Sprintf("Você ainda não está no time %s.", team.Name),
	}, nil
}

// verifyFirstChangeRequest looks for a pull or merge request authored by this
// person on the repository.
//
// Three things can leave it unable to answer, and all three report Pending
// rather than a failure: the person has never signed in through the provider,
// the organization has no token for that provider, or the provider is
// unreachable. Telling someone they have not opened a pull request when we
// simply could not look is worse than admitting we could not look.
func (v *OnboardingVerifier) verifyFirstChangeRequest(ctx context.Context, orgID, userID, repositoryID string) (models.OnboardingVerificationResult, error) {
	repo, err := v.repo.GetRepository(ctx, repositoryID)
	if err != nil {
		return models.OnboardingVerificationResult{}, err
	}
	if repo == nil || repo.OrganizationID != orgID {
		return models.OnboardingVerificationResult{
			Pending: true,
			How:     "O repositório que este passo verifica não existe mais.",
		}, nil
	}

	projectPath, provider, err := utils.ParseRepositoryURL(repo.URL)
	if err != nil {
		return models.OnboardingVerificationResult{
			Pending: true,
			How:     "A URL deste repositório não permite identificar o provedor.",
		}, nil
	}
	ref, err := scm.ParseRepoRef(projectPath)
	if err != nil {
		return models.OnboardingVerificationResult{
			Pending: true,
			How:     "A URL deste repositório não identifica um projeto.",
		}, nil
	}

	// The provider login, not our user id: change requests name their author by
	// login, which is why it is stored on the OAuth connection at all.
	conn, err := v.repo.GetOAuthConnectionByUser(ctx, userID, string(provider))
	if err != nil {
		return models.OnboardingVerificationResult{}, err
	}
	if conn == nil || strings.TrimSpace(conn.ProviderUsername) == "" {
		return models.OnboardingVerificationResult{
			Pending: true,
			How:     fmt.Sprintf("Entre uma vez com %s para a plataforma reconhecer seu usuário lá.", provider),
		}, nil
	}

	cfg, err := v.repo.GetOrganizationConfig(ctx, orgID)
	if err != nil {
		return models.OnboardingVerificationResult{}, err
	}
	client, err := v.resolve(provider, scm.CredentialsFromConfig(cfg, v.hosts))
	if err != nil {
		return models.OnboardingVerificationResult{
			Pending: true,
			How:     fmt.Sprintf("A organização ainda não configurou o acesso ao %s.", provider),
			Detail:  err.Error(),
		}, nil
	}

	changeRequests, err := client.ListChangeRequests(ctx, ref)
	if err != nil {
		utils.Warn("onboarding: could not list change requests for verification",
			"repository_id", repositoryID, "error", err)
		return models.OnboardingVerificationResult{
			Pending: true,
			How:     "Não foi possível consultar o provedor agora. Tente de novo em alguns minutos.",
			Detail:  err.Error(),
		}, nil
	}

	for i := range changeRequests {
		if strings.EqualFold(changeRequests[i].AuthorLogin, conn.ProviderUsername) {
			return models.OnboardingVerificationResult{
				Passed: true,
				How: fmt.Sprintf("Encontramos %s#%d, aberto por @%s.",
					repo.Name, changeRequests[i].Number, changeRequests[i].AuthorLogin),
			}, nil
		}
	}

	// Only open change requests are listed, so a merged first contribution is
	// invisible here. Saying which window was searched keeps the negative
	// honest instead of implying nothing was ever opened.
	return models.OnboardingVerificationResult{
		How: fmt.Sprintf("Nenhum pull/merge request aberto por @%s em %s neste momento.",
			conn.ProviderUsername, repo.Name),
	}, nil
}
