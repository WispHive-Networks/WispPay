package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/WispHive-Networks/WispPay/internal/billing"
	mysqlstore "github.com/WispHive-Networks/WispPay/internal/storage/mysql"
)

type workspaceStore interface {
	identityStore
	ListCategories(context.Context, string) ([]mysqlstore.Category, error)
	CreateCategory(context.Context, string, string) (mysqlstore.Category, error)
	GetCategory(context.Context, string, string) (mysqlstore.Category, error)
	UpdateCategory(context.Context, string, string, string) (mysqlstore.Category, error)
	ListProducts(context.Context, string) ([]mysqlstore.Product, error)
	CreateProduct(context.Context, string, mysqlstore.Product) (mysqlstore.Product, error)
	GetProduct(context.Context, string, string) (mysqlstore.Product, error)
	UpdateProduct(context.Context, string, string, mysqlstore.Product) (mysqlstore.Product, error)
	ListClients(context.Context, string) ([]mysqlstore.Client, error)
	CreateClient(context.Context, string, mysqlstore.Client) (mysqlstore.Client, error)
	GetClient(context.Context, string, string) (mysqlstore.Client, error)
	UpdateClient(context.Context, string, string, mysqlstore.Client) (mysqlstore.Client, error)
	CreateInvoice(context.Context, string, string, string, []mysqlstore.InvoiceItemInput) (mysqlstore.Invoice, error)
	ListInvoices(context.Context, string) ([]mysqlstore.Invoice, error)
	GetInvoice(context.Context, string, string) (mysqlstore.Invoice, error)
	GetPublicInvoice(context.Context, string) (mysqlstore.PublicInvoice, error)
	CreatePublicInvoiceLink(context.Context, string, string, string) (string, error)
	RevokePublicInvoiceLink(context.Context, string, string) error
}

type workspaceHandler struct {
	store      workspaceStore
	identity   identityStore
	publicBase string
}

var phoneCharacters = regexp.MustCompile(`^[+0-9(). -]{3,32}$`)

func registerWorkspaceRoutes(mux *http.ServeMux, store workspaceStore, publicBase string) {
	h := workspaceHandler{store: store, identity: store, publicBase: strings.TrimRight(publicBase, "/")}
	mux.HandleFunc("GET /api/catalog/categories", h.listCategories)
	mux.HandleFunc("POST /api/catalog/categories", h.createCategory)
	mux.HandleFunc("PATCH /api/catalog/categories/{id}", h.updateCategory)
	mux.HandleFunc("GET /api/catalog/products", h.listProducts)
	mux.HandleFunc("POST /api/catalog/products", h.createProduct)
	mux.HandleFunc("GET /api/catalog/products/{id}", h.getProduct)
	mux.HandleFunc("PATCH /api/catalog/products/{id}", h.updateProduct)
	mux.HandleFunc("GET /api/clients", h.listClients)
	mux.HandleFunc("POST /api/clients", h.createClient)
	mux.HandleFunc("GET /api/clients/{id}", h.getClient)
	mux.HandleFunc("PATCH /api/clients/{id}", h.updateClient)
	mux.HandleFunc("GET /api/invoices", h.listInvoices)
	mux.HandleFunc("POST /api/invoices", h.createInvoice)
	mux.HandleFunc("GET /api/invoices/{id}", h.getInvoice)
	mux.HandleFunc("POST /api/invoices/{id}/public-link", h.createPublicInvoiceLink)
	mux.HandleFunc("DELETE /api/invoices/{id}/public-link", h.revokePublicInvoiceLink)
	mux.HandleFunc("GET /api/public/invoices/{token}", h.getPublicInvoice)
}

func (h workspaceHandler) listCategories(w http.ResponseWriter, r *http.Request) {
	session, ok := h.privateRequest(w, r, false)
	if !ok {
		return
	}
	ctx, cancel := workspaceContext(r)
	defer cancel()
	items, err := h.store.ListCategories(ctx, session.BusinessID)
	h.respond(w, items, err, http.StatusOK)
}

func (h workspaceHandler) createCategory(w http.ResponseWriter, r *http.Request) {
	session, ok := h.privateRequest(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if len(input.Name) < 2 || len(input.Name) > 80 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "category name must be 2 to 80 characters"})
		return
	}
	ctx, cancel := workspaceContext(r)
	defer cancel()
	item, err := h.store.CreateCategory(ctx, session.BusinessID, input.Name)
	h.respond(w, item, err, http.StatusCreated)
}

func (h workspaceHandler) updateCategory(w http.ResponseWriter, r *http.Request) {
	session, ok := h.privateRequest(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if len(input.Name) < 2 || len(input.Name) > 80 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "category name must be 2 to 80 characters"})
		return
	}
	ctx, cancel := workspaceContext(r)
	defer cancel()
	item, err := h.store.UpdateCategory(ctx, session.BusinessID, r.PathValue("id"), input.Name)
	h.respond(w, item, err, http.StatusOK)
}

func (h workspaceHandler) listProducts(w http.ResponseWriter, r *http.Request) {
	session, ok := h.privateRequest(w, r, false)
	if !ok {
		return
	}
	ctx, cancel := workspaceContext(r)
	defer cancel()
	items, err := h.store.ListProducts(ctx, session.BusinessID)
	h.respond(w, items, err, http.StatusOK)
}

func (h workspaceHandler) createProduct(w http.ResponseWriter, r *http.Request) {
	session, ok := h.privateRequest(w, r, true)
	if !ok {
		return
	}
	input, valid := decodeProduct(w, r)
	if !valid {
		return
	}
	ctx, cancel := workspaceContext(r)
	defer cancel()
	item, err := h.store.CreateProduct(ctx, session.BusinessID, input)
	h.respond(w, item, err, http.StatusCreated)
}

func (h workspaceHandler) getProduct(w http.ResponseWriter, r *http.Request) {
	session, ok := h.privateRequest(w, r, false)
	if !ok {
		return
	}
	ctx, cancel := workspaceContext(r)
	defer cancel()
	item, err := h.store.GetProduct(ctx, session.BusinessID, r.PathValue("id"))
	h.respond(w, item, err, http.StatusOK)
}

func (h workspaceHandler) updateProduct(w http.ResponseWriter, r *http.Request) {
	session, ok := h.privateRequest(w, r, true)
	if !ok {
		return
	}
	input, valid := decodeProduct(w, r)
	if !valid {
		return
	}
	ctx, cancel := workspaceContext(r)
	defer cancel()
	item, err := h.store.UpdateProduct(ctx, session.BusinessID, r.PathValue("id"), input)
	h.respond(w, item, err, http.StatusOK)
}

func (h workspaceHandler) listClients(w http.ResponseWriter, r *http.Request) {
	session, ok := h.privateRequest(w, r, false)
	if !ok {
		return
	}
	ctx, cancel := workspaceContext(r)
	defer cancel()
	items, err := h.store.ListClients(ctx, session.BusinessID)
	h.respond(w, items, err, http.StatusOK)
}

func (h workspaceHandler) createClient(w http.ResponseWriter, r *http.Request) {
	session, ok := h.privateRequest(w, r, true)
	if !ok {
		return
	}
	input, valid := decodeClient(w, r)
	if !valid {
		return
	}
	ctx, cancel := workspaceContext(r)
	defer cancel()
	item, err := h.store.CreateClient(ctx, session.BusinessID, input)
	h.respond(w, item, err, http.StatusCreated)
}

func (h workspaceHandler) getClient(w http.ResponseWriter, r *http.Request) {
	session, ok := h.privateRequest(w, r, false)
	if !ok {
		return
	}
	ctx, cancel := workspaceContext(r)
	defer cancel()
	item, err := h.store.GetClient(ctx, session.BusinessID, r.PathValue("id"))
	h.respond(w, item, err, http.StatusOK)
}

func (h workspaceHandler) updateClient(w http.ResponseWriter, r *http.Request) {
	session, ok := h.privateRequest(w, r, true)
	if !ok {
		return
	}
	input, valid := decodeClient(w, r)
	if !valid {
		return
	}
	ctx, cancel := workspaceContext(r)
	defer cancel()
	item, err := h.store.UpdateClient(ctx, session.BusinessID, r.PathValue("id"), input)
	h.respond(w, item, err, http.StatusOK)
}

func (h workspaceHandler) listInvoices(w http.ResponseWriter, r *http.Request) {
	session, ok := h.privateRequest(w, r, false)
	if !ok {
		return
	}
	ctx, cancel := workspaceContext(r)
	defer cancel()
	items, err := h.store.ListInvoices(ctx, session.BusinessID)
	h.respond(w, items, err, http.StatusOK)
}

func (h workspaceHandler) createInvoice(w http.ResponseWriter, r *http.Request) {
	session, ok := h.privateRequest(w, r, true)
	if !ok {
		return
	}
	var input struct {
		ClientID string `json:"clientId"`
		Items    []struct {
			ProductID string `json:"productId"`
			Quantity  int    `json:"quantity"`
		} `json:"items"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	requested := make([]mysqlstore.InvoiceItemInput, 0, len(input.Items))
	for _, item := range input.Items {
		requested = append(requested, mysqlstore.InvoiceItemInput{ProductID: item.ProductID, Quantity: item.Quantity})
	}
	ctx, cancel := workspaceContext(r)
	defer cancel()
	invoice, err := h.store.CreateInvoice(ctx, session.BusinessID, input.ClientID, h.publicBase, requested)
	h.respond(w, invoice, err, http.StatusCreated)
}

func (h workspaceHandler) getInvoice(w http.ResponseWriter, r *http.Request) {
	session, ok := h.privateRequest(w, r, false)
	if !ok {
		return
	}
	ctx, cancel := workspaceContext(r)
	defer cancel()
	invoice, err := h.store.GetInvoice(ctx, session.BusinessID, r.PathValue("id"))
	h.respond(w, invoice, err, http.StatusOK)
}

func (h workspaceHandler) createPublicInvoiceLink(w http.ResponseWriter, r *http.Request) {
	session, ok := h.privateRequest(w, r, true)
	if !ok {
		return
	}
	ctx, cancel := workspaceContext(r)
	defer cancel()
	url, err := h.store.CreatePublicInvoiceLink(ctx, session.BusinessID, r.PathValue("id"), h.publicBase)
	if errors.Is(err, mysqlstore.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "invoice not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "public link could not be created"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"url": url})
}

func (h workspaceHandler) revokePublicInvoiceLink(w http.ResponseWriter, r *http.Request) {
	session, ok := h.privateRequest(w, r, true)
	if !ok {
		return
	}
	ctx, cancel := workspaceContext(r)
	defer cancel()
	if err := h.store.RevokePublicInvoiceLink(ctx, session.BusinessID, r.PathValue("id")); err != nil {
		if errors.Is(err, mysqlstore.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "invoice not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "public link could not be revoked"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h workspaceHandler) getPublicInvoice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private")
	if h.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "invoice service unavailable"})
		return
	}
	ctx, cancel := workspaceContext(r)
	defer cancel()
	invoice, err := h.store.GetPublicInvoice(ctx, r.PathValue("token"))
	if errors.Is(err, mysqlstore.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "invoice link is unavailable"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invoice could not be loaded"})
		return
	}
	writeJSON(w, http.StatusOK, invoice)
}

func (h workspaceHandler) privateRequest(w http.ResponseWriter, r *http.Request, mutation bool) (mysqlstore.SessionIdentity, bool) {
	identity := authHandler{store: h.identity}
	session, ok := identity.currentSession(w, r)
	if !ok {
		return mysqlstore.SessionIdentity{}, false
	}
	if mutation && !identity.validCSRF(w, r, session.CSRFHash) {
		return mysqlstore.SessionIdentity{}, false
	}
	return session, true
}

func (h workspaceHandler) respond(w http.ResponseWriter, body any, err error, success int) {
	if err == nil {
		writeJSON(w, success, body)
		return
	}
	switch {
	case errors.Is(err, mysqlstore.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "record not found"})
	case errors.Is(err, mysqlstore.ErrDuplicateName):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a record with that name already exists"})
	case errors.Is(err, mysqlstore.ErrInvalidInvoice):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invoice contains an invalid client, product, currency, or quantity"})
	case errors.Is(err, mysqlstore.ErrInvalidCategory):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "category is invalid for this business"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "request could not be completed"})
	}
}

func decodeProduct(w http.ResponseWriter, r *http.Request) (mysqlstore.Product, bool) {
	var input struct {
		CategoryID  *string `json:"categoryId"`
		Name        string  `json:"name"`
		Description string  `json:"description"`
		Price       string  `json:"price"`
		Currency    string  `json:"currency"`
		Available   *bool   `json:"available"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return mysqlstore.Product{}, false
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if input.CategoryID != nil {
		value := strings.TrimSpace(*input.CategoryID)
		if value == "" {
			input.CategoryID = nil
		} else {
			input.CategoryID = &value
		}
	}
	minor, err := billing.ParseMoney(input.Price)
	if len(input.Name) < 2 || len(input.Name) > 120 || len(input.Description) > 2000 || err != nil || !billing.ValidateCurrency(input.Currency) || input.Available == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "product name, nonnegative price, uppercase currency, and availability are required"})
		return mysqlstore.Product{}, false
	}
	return mysqlstore.Product{CategoryID: input.CategoryID, Name: input.Name, Description: input.Description,
		Price: billing.FormatMoney(minor), Currency: input.Currency, Available: *input.Available}, true
}

func decodeClient(w http.ResponseWriter, r *http.Request) (mysqlstore.Client, bool) {
	var input struct {
		DisplayName string `json:"displayName"`
		Email       string `json:"email"`
		Phone       string `json:"phone"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return mysqlstore.Client{}, false
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Phone = strings.TrimSpace(input.Phone)
	if len(input.DisplayName) < 2 || len(input.DisplayName) > 120 || len(input.Email) > 254 || len(input.Phone) > 32 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "client details are invalid"})
		return mysqlstore.Client{}, false
	}
	var email, phone *string
	if input.Email != "" {
		parsed, err := mail.ParseAddress(input.Email)
		if err != nil || parsed.Address != input.Email {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "client email is invalid"})
			return mysqlstore.Client{}, false
		}
		email = &input.Email
	}
	if input.Phone != "" {
		if !phoneCharacters.MatchString(input.Phone) || strings.Trim(input.Phone, "+0123456789(). -") != "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "client phone is invalid"})
			return mysqlstore.Client{}, false
		}
		phone = &input.Phone
	}
	return mysqlstore.Client{DisplayName: input.DisplayName, Email: email, Phone: phone}, true
}

func workspaceContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 10*time.Second)
}
