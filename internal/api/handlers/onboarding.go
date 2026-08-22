package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/onboarding"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// OnboardingHandler serves the configuration side of onboarding: the flows an
// admin composes, and the organization vocabulary its steps can reference.
type OnboardingHandler struct {
	onboarding *services.OnboardingService
	run        *services.OnboardingRunService
}

func NewOnboardingHandler(onboardingService *services.OnboardingService, runService *services.OnboardingRunService) *OnboardingHandler {
	return &OnboardingHandler{onboarding: onboardingService, run: runService}
}

// onboardingError maps the service's sentinels onto status codes. A reference
// from another organization is a 400 rather than a 403: the caller is telling us
// about an id they should not be using, and the fix is their payload.
func (h *OnboardingHandler) onboardingError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrOnboardingFlowNotFound):
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:            "not_found",
			ErrorDescription: "onboarding flow not found",
		})
	case errors.Is(err, services.ErrOnboardingAssignmentNotFound):
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:            "not_found",
			ErrorDescription: "you have no onboarding assigned for this step",
		})
	case errors.Is(err, services.ErrOnboardingStepNotFound):
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:            "not_found",
			ErrorDescription: "onboarding step not found",
		})
	case errors.Is(err, services.ErrOnboardingStepStatusInvalid):
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
	case errors.Is(err, services.ErrGlossaryTermNotFound):
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:            "not_found",
			ErrorDescription: "glossary term not found",
		})
	case errors.Is(err, services.ErrOnboardingFlowSlugTaken), errors.Is(err, services.ErrGlossaryTermTaken):
		c.JSON(http.StatusConflict, models.ErrorResponse{
			Error:            "conflict",
			ErrorDescription: err.Error(),
		})
	case errors.Is(err, services.ErrUserNotInOrganization):
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "that user does not belong to this organization",
		})
	case errors.Is(err, services.ErrOnboardingFlowInvalid),
		errors.Is(err, services.ErrOnboardingStepInvalid),
		errors.Is(err, services.ErrOnboardingReferenceNotInOrganization),
		errors.Is(err, services.ErrOnboardingTemplateNotFound),
		errors.Is(err, services.ErrGlossaryTermInvalid):
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
	default:
		utils.Error("onboarding handler: unexpected failure", "error", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "internal_error",
			ErrorDescription: "failed to process the onboarding request",
		})
	}
}

// ListFlows lists the organization's onboarding flows.
// @Summary      List onboarding flows
// @Tags         onboarding
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} models.OnboardingFlowListResponse
// @Failure      401 {object} models.ErrorResponse
// @Router       /onboarding/flows [get]
func (h *OnboardingHandler) ListFlows(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}

	flows, counts, err := h.onboarding.ListFlows(c.Request.Context(), orgID)
	if err != nil {
		h.onboardingError(c, err)
		return
	}

	items := make([]models.OnboardingFlowResponse, 0, len(flows))
	for i := range flows {
		items = append(items, models.OnboardingFlowToResponse(&flows[i], counts[flows[i].ID]))
	}
	c.JSON(http.StatusOK, models.OnboardingFlowListResponse{Items: items, Total: len(items)})
}

// GetFlow returns one flow with its steps.
// @Summary      Get an onboarding flow
// @Tags         onboarding
// @Produce      json
// @Security     BearerAuth
// @Param        id  path string true "Flow ID"
// @Success      200 {object} models.OnboardingFlowResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /onboarding/flows/{id} [get]
func (h *OnboardingHandler) GetFlow(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}

	flow, err := h.onboarding.GetFlow(c.Request.Context(), orgID, c.Param("id"))
	if err != nil {
		h.onboardingError(c, err)
		return
	}
	c.JSON(http.StatusOK, models.OnboardingFlowToResponse(flow, len(flow.Steps)))
}

// CreateFlow creates a flow, optionally seeded from a starter template.
// @Summary      Create an onboarding flow
// @Tags         onboarding
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body body models.CreateOnboardingFlowRequest true "Flow"
// @Success      201 {object} models.OnboardingFlowResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse
// @Router       /onboarding/flows [post]
func (h *OnboardingHandler) CreateFlow(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	actorID, _ := utils.GetUserIDFromContext(c)

	var req models.CreateOnboardingFlowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	flow, err := h.onboarding.CreateFlow(c.Request.Context(), orgID, actorID, req)
	if err != nil {
		h.onboardingError(c, err)
		return
	}
	c.JSON(http.StatusCreated, models.OnboardingFlowToResponse(flow, len(flow.Steps)))
}

// UpdateFlow renames a flow or changes which one is the default.
// @Summary      Update an onboarding flow
// @Tags         onboarding
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id   path string true "Flow ID"
// @Param        body body models.UpdateOnboardingFlowRequest true "Fields to change"
// @Success      200 {object} models.OnboardingFlowResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /onboarding/flows/{id} [patch]
func (h *OnboardingHandler) UpdateFlow(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}

	var req models.UpdateOnboardingFlowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	flow, err := h.onboarding.UpdateFlow(c.Request.Context(), orgID, c.Param("id"), req)
	if err != nil {
		h.onboardingError(c, err)
		return
	}
	c.JSON(http.StatusOK, models.OnboardingFlowToResponse(flow, 0))
}

// DeleteFlow soft-deletes a flow.
// @Summary      Delete an onboarding flow
// @Tags         onboarding
// @Security     BearerAuth
// @Param        id path string true "Flow ID"
// @Success      204
// @Failure      404 {object} models.ErrorResponse
// @Router       /onboarding/flows/{id} [delete]
func (h *OnboardingHandler) DeleteFlow(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}

	if err := h.onboarding.DeleteFlow(c.Request.Context(), orgID, c.Param("id")); err != nil {
		h.onboardingError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// DuplicateFlow copies a flow and its steps.
// @Summary      Duplicate an onboarding flow
// @Tags         onboarding
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Flow ID"
// @Success      201 {object} models.OnboardingFlowResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /onboarding/flows/{id}/duplicate [post]
func (h *OnboardingHandler) DuplicateFlow(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	actorID, _ := utils.GetUserIDFromContext(c)

	flow, err := h.onboarding.DuplicateFlow(c.Request.Context(), orgID, actorID, c.Param("id"))
	if err != nil {
		h.onboardingError(c, err)
		return
	}
	c.JSON(http.StatusCreated, models.OnboardingFlowToResponse(flow, len(flow.Steps)))
}

// ReplaceSteps saves the whole step list of a flow.
//
// Array order is the flow's order, so this is also how steps are reordered —
// there is no separate reorder endpoint to keep consistent with this one.
//
// @Summary      Replace the steps of an onboarding flow
// @Tags         onboarding
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id   path string true "Flow ID"
// @Param        body body models.ReplaceOnboardingStepsRequest true "Steps, in order"
// @Success      200 {object} models.OnboardingFlowResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /onboarding/flows/{id}/steps [put]
func (h *OnboardingHandler) ReplaceSteps(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}

	var req models.ReplaceOnboardingStepsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	flowID := c.Param("id")
	steps, err := h.onboarding.ReplaceSteps(c.Request.Context(), orgID, flowID, req.Steps)
	if err != nil {
		h.onboardingError(c, err)
		return
	}

	flow, err := h.onboarding.GetFlow(c.Request.Context(), orgID, flowID)
	if err != nil {
		h.onboardingError(c, err)
		return
	}
	c.JSON(http.StatusOK, models.OnboardingFlowToResponse(flow, len(steps)))
}

// ListTemplates returns the starter flows an admin can create from.
// @Summary      List onboarding templates
// @Tags         onboarding
// @Produce      json
// @Security     BearerAuth
// @Success      200 {array} onboarding.Template
// @Router       /onboarding/templates [get]
func (h *OnboardingHandler) ListTemplates(c *gin.Context) {
	if _, ok := requireOrganization(c); !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": h.onboarding.Templates(), "total": len(h.onboarding.Templates())})
}

// AssignFlow gives a flow to a member who is already in the organization.
//
// The invite path assigns automatically; this is for the person who changed
// teams, or who joined before anyone wrote an onboarding.
//
// @Summary      Assign an onboarding flow to a member
// @Tags         onboarding
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body body models.AssignOnboardingRequest true "Flow and member"
// @Success      201 {object} models.OnboardingAssignmentSummary
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /onboarding/assignments [post]
func (h *OnboardingHandler) AssignFlow(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	actorID, _ := utils.GetUserIDFromContext(c)

	var req models.AssignOnboardingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	assignment, err := h.onboarding.AssignToMember(c.Request.Context(), orgID, actorID, req)
	if err != nil {
		h.onboardingError(c, err)
		return
	}

	c.JSON(http.StatusCreated, models.OnboardingAssignmentSummary{
		ID:        assignment.ID,
		FlowID:    assignment.FlowID,
		UserID:    assignment.UserID,
		Status:    assignment.Status,
		CreatedAt: assignment.CreatedAt,
	})
}

// ListAssignments is the progress dashboard: who is onboarding, how far along,
// and what they said was missing.
// @Summary      List onboarding assignments
// @Tags         onboarding
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} models.OnboardingAssignmentListResponse
// @Failure      403 {object} models.ErrorResponse
// @Router       /onboarding/assignments [get]
func (h *OnboardingHandler) ListAssignments(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}

	items, err := h.onboarding.ListAssignments(c.Request.Context(), orgID)
	if err != nil {
		h.onboardingError(c, err)
		return
	}
	c.JSON(http.StatusOK, models.OnboardingAssignmentListResponse{Items: items, Total: len(items)})
}

// ── the runner ───────────────────────────────────────────────────────────────

// MyOnboarding returns the caller's own onboarding, with every step already
// resolved against live data.
// @Summary      Get my onboarding
// @Tags         onboarding
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} models.OnboardingRunListResponse
// @Failure      401 {object} models.ErrorResponse
// @Router       /onboarding/me [get]
func (h *OnboardingHandler) MyOnboarding(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	userID, err := utils.GetUserIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing or invalid authentication",
		})
		return
	}

	runs, err := h.run.ListForUser(c.Request.Context(), orgID, userID)
	if err != nil {
		h.onboardingError(c, err)
		return
	}
	c.JSON(http.StatusOK, models.OnboardingRunListResponse{Items: runs, Total: len(runs)})
}

// MarkStep records an outcome for one step of the caller's onboarding.
// @Summary      Mark an onboarding step
// @Tags         onboarding
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        stepID path string true "Step ID"
// @Param        body body models.MarkOnboardingStepRequest true "Outcome"
// @Success      200 {object} models.OnboardingRunResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /onboarding/me/steps/{stepID} [post]
func (h *OnboardingHandler) MarkStep(c *gin.Context) {
	orgID, userID, ok := h.requireCaller(c)
	if !ok {
		return
	}

	var req models.MarkOnboardingStepRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	run, err := h.run.MarkStep(c.Request.Context(), orgID, userID, c.Param("stepID"), req)
	if err != nil {
		h.onboardingError(c, err)
		return
	}
	c.JSON(http.StatusOK, run)
}

// VerifyStep runs the check behind a verified step.
//
// On demand rather than on load: the check talks to the provider, and running
// it on every render would spend latency and rate limit to learn nothing new.
//
// @Summary      Run a verified onboarding step's check
// @Tags         onboarding
// @Produce      json
// @Security     BearerAuth
// @Param        stepID path string true "Step ID"
// @Success      200 {object} models.OnboardingVerificationResult
// @Failure      404 {object} models.ErrorResponse
// @Router       /onboarding/me/steps/{stepID}/verify [post]
func (h *OnboardingHandler) VerifyStep(c *gin.Context) {
	orgID, userID, ok := h.requireCaller(c)
	if !ok {
		return
	}

	result, err := h.run.Verify(c.Request.Context(), orgID, userID, c.Param("stepID"))
	if err != nil {
		h.onboardingError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// SubmitFeedback stores what the newcomer says was missing.
// @Summary      Leave onboarding feedback
// @Tags         onboarding
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        assignmentID path string true "Assignment ID"
// @Param        body body models.OnboardingFeedbackRequest true "Feedback"
// @Success      204
// @Failure      404 {object} models.ErrorResponse
// @Router       /onboarding/me/assignments/{assignmentID}/feedback [post]
func (h *OnboardingHandler) SubmitFeedback(c *gin.Context) {
	orgID, userID, ok := h.requireCaller(c)
	if !ok {
		return
	}

	var req models.OnboardingFeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	if err := h.run.SubmitFeedback(c.Request.Context(), orgID, userID, c.Param("assignmentID"), req); err != nil {
		h.onboardingError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// requireCaller resolves the organization and the user behind the request. The
// runner endpoints act on the caller's own onboarding and nobody else's, which
// is why they never take a user id from the payload.
func (h *OnboardingHandler) requireCaller(c *gin.Context) (string, string, bool) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return "", "", false
	}
	userID, err := utils.GetUserIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing or invalid authentication",
		})
		return "", "", false
	}
	return orgID, userID, true
}

// ── glossary ─────────────────────────────────────────────────────────────────

// ListGlossaryTerms lists the organization's vocabulary.
// @Summary      List glossary terms
// @Tags         glossary
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} models.GlossaryTermListResponse
// @Router       /glossary [get]
func (h *OnboardingHandler) ListGlossaryTerms(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}

	terms, err := h.onboarding.ListGlossaryTerms(c.Request.Context(), orgID)
	if err != nil {
		h.onboardingError(c, err)
		return
	}

	items := make([]models.GlossaryTermResponse, 0, len(terms))
	for i := range terms {
		items = append(items, models.GlossaryTermToResponse(&terms[i]))
	}
	c.JSON(http.StatusOK, models.GlossaryTermListResponse{Items: items, Total: len(items)})
}

// CreateGlossaryTerm adds a term.
// @Summary      Create a glossary term
// @Tags         glossary
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body body models.CreateGlossaryTermRequest true "Term"
// @Success      201 {object} models.GlossaryTermResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse
// @Router       /glossary [post]
func (h *OnboardingHandler) CreateGlossaryTerm(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}
	actorID, _ := utils.GetUserIDFromContext(c)

	var req models.CreateGlossaryTermRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	term, err := h.onboarding.CreateGlossaryTerm(c.Request.Context(), orgID, actorID, req)
	if err != nil {
		h.onboardingError(c, err)
		return
	}
	c.JSON(http.StatusCreated, models.GlossaryTermToResponse(term))
}

// UpdateGlossaryTerm edits a term.
// @Summary      Update a glossary term
// @Tags         glossary
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id   path string true "Term ID"
// @Param        body body models.UpdateGlossaryTermRequest true "Fields to change"
// @Success      200 {object} models.GlossaryTermResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /glossary/{id} [patch]
func (h *OnboardingHandler) UpdateGlossaryTerm(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}

	var req models.UpdateGlossaryTermRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
		})
		return
	}

	term, err := h.onboarding.UpdateGlossaryTerm(c.Request.Context(), orgID, c.Param("id"), req)
	if err != nil {
		h.onboardingError(c, err)
		return
	}
	c.JSON(http.StatusOK, models.GlossaryTermToResponse(term))
}

// DeleteGlossaryTerm removes a term.
//
// Soft-deleted, so a step that referenced it renders the reference as gone
// instead of breaking the flow.
//
// @Summary      Delete a glossary term
// @Tags         glossary
// @Security     BearerAuth
// @Param        id path string true "Term ID"
// @Success      204
// @Failure      404 {object} models.ErrorResponse
// @Router       /glossary/{id} [delete]
func (h *OnboardingHandler) DeleteGlossaryTerm(c *gin.Context) {
	orgID, ok := requireOrganization(c)
	if !ok {
		return
	}

	if err := h.onboarding.DeleteGlossaryTerm(c.Request.Context(), orgID, c.Param("id")); err != nil {
		h.onboardingError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// compile-time assurance that the template registry stays reachable from the
// handler's Swagger annotation above.
var _ = onboarding.Templates
