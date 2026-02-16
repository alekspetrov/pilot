package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/alekspetrov/pilot/cloud/internal/auth"
	"github.com/alekspetrov/pilot/cloud/internal/billing"
	"github.com/alekspetrov/pilot/cloud/internal/oauth"
	"github.com/alekspetrov/pilot/cloud/internal/research"
	"github.com/alekspetrov/pilot/cloud/internal/sandbox"
	"github.com/alekspetrov/pilot/cloud/internal/tenants"
)

// Server handles HTTP requests
type Server struct {
	tenantService   *tenants.Service
	oauthService    *oauth.Service
	billingService  *billing.Service
	researchService *research.Service
	executor        *sandbox.Executor
	tokenService    *auth.TokenService
}

// NewServer creates a new API server
func NewServer(
	tenantService *tenants.Service,
	oauthService *oauth.Service,
	billingService *billing.Service,
	researchService *research.Service,
	executor *sandbox.Executor,
	tokenService *auth.TokenService,
) *Server {
	return &Server{
		tenantService:   tenantService,
		oauthService:    oauthService,
		billingService:  billingService,
		researchService: researchService,
		executor:        executor,
		tokenService:    tokenService,
	}
}

// Router returns the HTTP router
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(corsMiddleware)

	// Health check
	r.Get("/health", s.healthCheck)

	// Public routes
	r.Route("/auth", func(r chi.Router) {
		r.Post("/signup", s.signup)
		r.Post("/login", s.login)
		r.Post("/refresh", s.refreshToken)
	})

	// OAuth callbacks (no auth required)
	r.Route("/oauth", func(r chi.Router) {
		r.Get("/callback/{provider}", s.oauthCallback)
	})

	// Stripe webhook (no auth, uses signature verification)
	r.Post("/webhooks/stripe", s.stripeWebhook)

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(s.tokenService.AuthMiddleware)

		// User profile
		r.Get("/me", s.getProfile)
		r.Put("/me", s.updateProfile)

		// Organizations
		r.Route("/organizations", func(r chi.Router) {
			r.Get("/", s.listOrganizations)
			r.Post("/", s.createOrganization)
		})

		// Organization-scoped routes
		r.Route("/orgs/{orgID}", func(r chi.Router) {
			r.Use(s.orgMiddleware)

			r.Get("/", s.getOrganization)
			r.Put("/", s.updateOrganization)

			// Members
			r.Route("/members", func(r chi.Router) {
				r.Get("/", s.listMembers)
				r.Post("/invite", s.inviteMember)
				r.Delete("/{userID}", s.removeMember)
				r.Put("/{userID}/role", s.updateMemberRole)
			})

			// Projects
			r.Route("/projects", func(r chi.Router) {
				r.Get("/", s.listProjects)
				r.Post("/", s.createProject)
				r.Get("/{projectID}", s.getProject)
				r.Put("/{projectID}", s.updateProject)
				r.Delete("/{projectID}", s.deleteProject)

				// Executions for a project
				r.Get("/{projectID}/executions", s.listProjectExecutions)
			})

			// Executions
			r.Route("/executions", func(r chi.Router) {
				r.Get("/", s.listExecutions)
				r.Post("/", s.createExecution)
				r.Get("/{executionID}", s.getExecution)
				r.Post("/{executionID}/cancel", s.cancelExecution)
			})

			// Integrations (OAuth)
			r.Route("/integrations", func(r chi.Router) {
				r.Get("/", s.listIntegrations)
				r.Get("/{provider}/auth", s.initiateOAuth)
				r.Delete("/{provider}", s.disconnectIntegration)
			})

			// Billing
			r.Route("/billing", func(r chi.Router) {
				r.Get("/usage", s.getUsageSummary)
				r.Get("/invoices", s.listInvoices)
				r.Post("/checkout", s.createCheckoutSession)
				r.Get("/portal", s.createPortalSession)
				r.Post("/cancel", s.cancelSubscription)
			})

			// Researches
			r.Route("/researches", func(r chi.Router) {
				r.Get("/", s.listResearches)
				r.Post("/", s.createResearch)
				r.Get("/{researchID}", s.getResearch)
				r.Put("/{researchID}", s.updateResearch)
				r.Delete("/{researchID}", s.deleteResearch)

				// Own app
				r.Put("/{researchID}/own-app", s.setOwnApp)

				// Notes
				r.Route("/{researchID}/notes", func(r chi.Router) {
					r.Get("/", s.listNotes)
					r.Post("/", s.createNote)
					r.Get("/{noteID}", s.getNote)
					r.Put("/{noteID}", s.updateNote)
					r.Delete("/{noteID}", s.deleteNote)
				})

				// Competitors
				r.Route("/{researchID}/competitors", func(r chi.Router) {
					r.Get("/", s.listCompetitors)
					r.Post("/", s.addCompetitor)
					r.Get("/{competitorID}", s.getCompetitor)
					r.Put("/{competitorID}", s.updateCompetitor)
					r.Delete("/{competitorID}", s.deleteCompetitor)
				})
			})

			// Audit logs
			r.Get("/audit-logs", s.listAuditLogs)
		})
	})

	return r
}

// Middleware

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-Org-ID, X-API-Key")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) orgMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orgIDStr := chi.URLParam(r, "orgID")
		orgID, err := uuid.Parse(orgIDStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid organization ID")
			return
		}

		userID, ok := auth.GetUserID(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		// Check membership
		if err := s.tenantService.CheckPermission(r.Context(), orgID, userID, tenants.RoleViewer); err != nil {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}

		ctx := r.Context()
		ctx = setOrgID(ctx, orgID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Health check
func (s *Server) healthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Auth handlers

func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name         string `json:"name"`
		Email        string `json:"email"`
		Password     string `json:"password"`
		Organization string `json:"organization"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	org, user, err := s.tenantService.CreateOrganizationWithOwner(r.Context(), tenants.CreateOrgInput{
		Name:      input.Organization,
		OwnerName: input.Name,
		Email:     input.Email,
		Password:  input.Password,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tokens, err := s.tokenService.GenerateTokenPair(user.ID, user.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate tokens")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"user":         user,
		"organization": org,
		"tokens":       tokens,
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := s.tenantService.Authenticate(r.Context(), input.Email, input.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	tokens, err := s.tokenService.GenerateTokenPair(user.ID, user.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate tokens")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user":   user,
		"tokens": tokens,
	})
}

func (s *Server) refreshToken(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RefreshToken string `json:"refresh_token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	claims, err := s.tokenService.ValidateToken(input.RefreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}

	tokens, err := s.tokenService.GenerateTokenPair(claims.UserID, claims.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate tokens")
		return
	}

	writeJSON(w, http.StatusOK, tokens)
}

// Profile handlers

func (s *Server) getProfile(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.GetUserID(r.Context())
	// Get user from store (not implemented in this example)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user_id": userID,
	})
}

func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// Organization handlers

func (s *Server) listOrganizations(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.GetUserID(r.Context())
	// This would call the store to list orgs
	_ = userID
	writeJSON(w, http.StatusOK, []interface{}{})
}

func (s *Server) createOrganization(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Create org logic would go here
	writeJSON(w, http.StatusCreated, map[string]string{"name": input.Name})
}

func (s *Server) getOrganization(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgIDFromContext(r.Context())
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": orgID,
	})
}

func (s *Server) updateOrganization(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// Member handlers

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []interface{}{})
}

func (s *Server) inviteMember(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgIDFromContext(r.Context())
	userID, _ := auth.GetUserID(r.Context())

	var input struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	inv, err := s.tenantService.InviteMember(r.Context(), orgID, userID, input.Email, tenants.Role(input.Role))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, inv)
}

func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) updateMemberRole(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// Project handlers

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []interface{}{})
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgIDFromContext(r.Context())
	userID, _ := auth.GetUserID(r.Context())

	var input struct {
		Name    string `json:"name"`
		RepoURL string `json:"repo_url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	project, err := s.tenantService.AddProject(r.Context(), orgID, userID, input.Name, input.RepoURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, project)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{})
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) listProjectExecutions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []interface{}{})
}

// Execution handlers

func (s *Server) listExecutions(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgIDFromContext(r.Context())

	limit := 50
	offset := 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}

	executions, err := s.executor.ListExecutions(r.Context(), orgID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, executions)
}

func (s *Server) createExecution(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgIDFromContext(r.Context())

	var input struct {
		ProjectID      string `json:"project_id"`
		ExternalTaskID string `json:"external_task_id,omitempty"`
		Prompt         string `json:"prompt"`
		Branch         string `json:"branch,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	projectID, err := uuid.Parse(input.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project ID")
		return
	}

	// Check quota before submitting
	hasQuota, err := s.billingService.CheckQuota(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !hasQuota {
		writeError(w, http.StatusPaymentRequired, "quota exceeded")
		return
	}

	execution, err := s.executor.Submit(r.Context(), sandbox.ExecutionRequest{
		OrgID:          orgID,
		ProjectID:      projectID,
		ExternalTaskID: input.ExternalTaskID,
		Prompt:         input.Prompt,
		Branch:         input.Branch,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Record usage
	_ = s.billingService.RecordUsage(r.Context(), orgID, &execution.ID, billing.UsageTypeTask, 1)

	writeJSON(w, http.StatusCreated, execution)
}

func (s *Server) getExecution(w http.ResponseWriter, r *http.Request) {
	executionIDStr := chi.URLParam(r, "executionID")
	executionID, err := uuid.Parse(executionIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid execution ID")
		return
	}

	execution, err := s.executor.GetExecution(r.Context(), executionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "execution not found")
		return
	}

	writeJSON(w, http.StatusOK, execution)
}

func (s *Server) cancelExecution(w http.ResponseWriter, r *http.Request) {
	executionIDStr := chi.URLParam(r, "executionID")
	executionID, err := uuid.Parse(executionIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid execution ID")
		return
	}

	if err := s.executor.Cancel(r.Context(), executionID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// Integration handlers

func (s *Server) listIntegrations(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgIDFromContext(r.Context())

	integrations, err := s.oauthService.ListIntegrations(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, integrations)
}

func (s *Server) initiateOAuth(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgIDFromContext(r.Context())
	userID, _ := auth.GetUserID(r.Context())
	provider := oauth.Provider(chi.URLParam(r, "provider"))

	redirectURL := r.URL.Query().Get("redirect_url")
	if redirectURL == "" {
		redirectURL = "/settings/integrations"
	}

	authURL, err := s.oauthService.GetAuthorizationURL(r.Context(), orgID, userID, provider, redirectURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"auth_url": authURL})
}

func (s *Server) oauthCallback(w http.ResponseWriter, r *http.Request) {
	provider := oauth.Provider(chi.URLParam(r, "provider"))
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	_, redirectURL, err := s.oauthService.HandleCallback(r.Context(), provider, code, state)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func (s *Server) disconnectIntegration(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgIDFromContext(r.Context())
	provider := oauth.Provider(chi.URLParam(r, "provider"))

	if err := s.oauthService.DisconnectIntegration(r.Context(), orgID, provider); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

// Billing handlers

func (s *Server) getUsageSummary(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgIDFromContext(r.Context())

	summary, err := s.billingService.GetUsageSummary(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) listInvoices(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgIDFromContext(r.Context())

	invoices, err := s.billingService.GetInvoices(r.Context(), orgID, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, invoices)
}

func (s *Server) createCheckoutSession(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgIDFromContext(r.Context())
	email, _ := auth.GetUserEmail(r.Context())

	var input struct {
		PlanID string `json:"plan_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	session, err := s.billingService.CreateCheckoutSession(r.Context(), orgID, input.PlanID, email)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, session)
}

func (s *Server) createPortalSession(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgIDFromContext(r.Context())

	session, err := s.billingService.CreatePortalSession(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, session)
}

func (s *Server) cancelSubscription(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgIDFromContext(r.Context())

	if err := s.billingService.CancelSubscription(r.Context(), orgID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancellation scheduled"})
}

func (s *Server) stripeWebhook(w http.ResponseWriter, r *http.Request) {
	payload := make([]byte, r.ContentLength)
	_, _ = r.Body.Read(payload)

	signature := r.Header.Get("Stripe-Signature")

	if err := s.billingService.HandleWebhook(r.Context(), payload, signature); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "received"})
}

// Audit logs handler

func (s *Server) listAuditLogs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []interface{}{})
}

// Research handlers

func (s *Server) listResearches(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgIDFromContext(r.Context())

	limit := 50
	offset := 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil {
			offset = parsed
		}
	}

	researches, err := s.researchService.ListResearches(r.Context(), orgID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, researches)
}

func (s *Server) createResearch(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgIDFromContext(r.Context())
	userID, _ := auth.GetUserID(r.Context())

	var input research.CreateResearchInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	res, err := s.researchService.CreateResearch(r.Context(), orgID, userID, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) getResearch(w http.ResponseWriter, r *http.Request) {
	researchIDStr := chi.URLParam(r, "researchID")
	researchID, err := uuid.Parse(researchIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid research ID")
		return
	}

	res, notes, competitors, err := s.researchService.GetResearchWithDetails(r.Context(), researchID)
	if err != nil {
		writeError(w, http.StatusNotFound, "research not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"research":    res,
		"notes":       notes,
		"competitors": competitors,
	})
}

func (s *Server) updateResearch(w http.ResponseWriter, r *http.Request) {
	researchIDStr := chi.URLParam(r, "researchID")
	researchID, err := uuid.Parse(researchIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid research ID")
		return
	}

	var input research.UpdateResearchInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	res, err := s.researchService.UpdateResearch(r.Context(), researchID, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, res)
}

func (s *Server) deleteResearch(w http.ResponseWriter, r *http.Request) {
	researchIDStr := chi.URLParam(r, "researchID")
	researchID, err := uuid.Parse(researchIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid research ID")
		return
	}

	if err := s.researchService.DeleteResearch(r.Context(), researchID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) setOwnApp(w http.ResponseWriter, r *http.Request) {
	researchIDStr := chi.URLParam(r, "researchID")
	researchID, err := uuid.Parse(researchIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid research ID")
		return
	}

	var input struct {
		AppID       string   `json:"app_id"`
		AppName     string   `json:"app_name"`
		IconURL     string   `json:"icon_url"`
		Screenshots []string `json:"screenshots"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	res, err := s.researchService.SetOwnApp(r.Context(), researchID, input.AppID, input.AppName, input.IconURL, input.Screenshots)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, res)
}

// Note handlers

func (s *Server) listNotes(w http.ResponseWriter, r *http.Request) {
	researchIDStr := chi.URLParam(r, "researchID")
	researchID, err := uuid.Parse(researchIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid research ID")
		return
	}

	category := r.URL.Query().Get("category")
	var notes []*research.OwnAppNote

	if category != "" {
		notes, err = s.researchService.ListNotesByCategory(r.Context(), researchID, research.NoteCategory(category))
	} else {
		notes, err = s.researchService.ListNotes(r.Context(), researchID)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, notes)
}

func (s *Server) createNote(w http.ResponseWriter, r *http.Request) {
	researchIDStr := chi.URLParam(r, "researchID")
	researchID, err := uuid.Parse(researchIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid research ID")
		return
	}

	userID, _ := auth.GetUserID(r.Context())

	var input research.CreateNoteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	note, err := s.researchService.CreateNote(r.Context(), researchID, userID, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, note)
}

func (s *Server) getNote(w http.ResponseWriter, r *http.Request) {
	noteIDStr := chi.URLParam(r, "noteID")
	noteID, err := uuid.Parse(noteIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid note ID")
		return
	}

	note, err := s.researchService.GetNote(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}

	writeJSON(w, http.StatusOK, note)
}

func (s *Server) updateNote(w http.ResponseWriter, r *http.Request) {
	noteIDStr := chi.URLParam(r, "noteID")
	noteID, err := uuid.Parse(noteIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid note ID")
		return
	}

	var input research.UpdateNoteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	note, err := s.researchService.UpdateNote(r.Context(), noteID, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, note)
}

func (s *Server) deleteNote(w http.ResponseWriter, r *http.Request) {
	noteIDStr := chi.URLParam(r, "noteID")
	noteID, err := uuid.Parse(noteIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid note ID")
		return
	}

	if err := s.researchService.DeleteNote(r.Context(), noteID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// Competitor handlers

func (s *Server) listCompetitors(w http.ResponseWriter, r *http.Request) {
	researchIDStr := chi.URLParam(r, "researchID")
	researchID, err := uuid.Parse(researchIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid research ID")
		return
	}

	competitors, err := s.researchService.ListCompetitors(r.Context(), researchID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, competitors)
}

func (s *Server) addCompetitor(w http.ResponseWriter, r *http.Request) {
	researchIDStr := chi.URLParam(r, "researchID")
	researchID, err := uuid.Parse(researchIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid research ID")
		return
	}

	var input research.AddCompetitorInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	competitor, err := s.researchService.AddCompetitor(r.Context(), researchID, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, competitor)
}

func (s *Server) getCompetitor(w http.ResponseWriter, r *http.Request) {
	competitorIDStr := chi.URLParam(r, "competitorID")
	competitorID, err := uuid.Parse(competitorIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid competitor ID")
		return
	}

	competitor, err := s.researchService.GetCompetitor(r.Context(), competitorID)
	if err != nil {
		writeError(w, http.StatusNotFound, "competitor not found")
		return
	}

	writeJSON(w, http.StatusOK, competitor)
}

func (s *Server) updateCompetitor(w http.ResponseWriter, r *http.Request) {
	competitorIDStr := chi.URLParam(r, "competitorID")
	competitorID, err := uuid.Parse(competitorIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid competitor ID")
		return
	}

	competitor, err := s.researchService.GetCompetitor(r.Context(), competitorID)
	if err != nil {
		writeError(w, http.StatusNotFound, "competitor not found")
		return
	}

	var input struct {
		Name        *string  `json:"name,omitempty"`
		IconURL     *string  `json:"icon_url,omitempty"`
		Screenshots []string `json:"screenshots,omitempty"`
		Notes       []string `json:"notes,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if input.Name != nil {
		competitor.Name = *input.Name
	}
	if input.IconURL != nil {
		competitor.IconURL = *input.IconURL
	}
	if input.Screenshots != nil {
		competitor.Screenshots = input.Screenshots
	}
	if input.Notes != nil {
		competitor.Notes = input.Notes
	}

	if err := s.researchService.UpdateCompetitor(r.Context(), competitor); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, competitor)
}

func (s *Server) deleteCompetitor(w http.ResponseWriter, r *http.Request) {
	competitorIDStr := chi.URLParam(r, "competitorID")
	competitorID, err := uuid.Parse(competitorIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid competitor ID")
		return
	}

	if err := s.researchService.DeleteCompetitor(r.Context(), competitorID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// Helper functions

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

type orgContextKey struct{}

func setOrgID(ctx context.Context, orgID uuid.UUID) context.Context {
	return context.WithValue(ctx, orgContextKey{}, orgID)
}

func getOrgIDFromContext(ctx context.Context) uuid.UUID {
	if orgID, ok := ctx.Value(orgContextKey{}).(uuid.UUID); ok {
		return orgID
	}
	return uuid.Nil
}
