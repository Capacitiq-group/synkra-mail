// handlers_billing.go — billing endpoints (phase 11):
//
//	GET  /org/billing                 plan + subscription + contact + methods
//	PUT  /org/billing/contact         billing contact (email, name, VAT number)
//	GET  /org/billing/preview         line-item preview (plan × mailboxes + overage)
//	POST /org/billing/invoices        generate an invoice from the preview
//	GET  /org/billing/invoices        payment history + invoice list
//	GET  /org/billing/invoices/{id}   one invoice (per-line VAT)
//	POST /org/billing/invoices/{id}/pay   Paystack checkout (or honest 501)
//	POST /org/billing/invoices/{id}/verify  resolve the checkout result
//	POST /org/billing/invoices/{id}/mark-paid  dev-only manual capture
//	POST /org/billing/payment-methods     store a verified card's masked view
//	DELETE /org/billing/payment-methods/{id}
package api

import (
	"encoding/json"
	"math"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"synkra.email/mail/internal/ids"
	"synkra.email/mail/internal/paystack"
	"synkra.email/mail/internal/store"
)

// invoiceLineItems builds the plan × mailboxes + storage-overage lines.
func (s *Server) invoiceLineItems(r *http.Request, orgID uuid.UUID) ([]store.InvoiceLine, error) {
	sub, err := store.GetActiveSubscription(r.Context(), s.Store.Pool, orgID)
	if err != nil {
		return nil, err
	}
	plan, err := store.GetPlanByID(r.Context(), s.Store.Pool, sub.PlanID)
	if err != nil {
		return nil, err
	}
	mailboxes, err := store.CountOrgMailboxes(r.Context(), s.Store.Pool, orgID)
	if err != nil {
		return nil, err
	}
	if mailboxes < 1 {
		mailboxes = 1
	}
	unit := float64(plan.PriceCents) / 100
	perMailbox := math.Round(unit*100) / 100
	lines := []store.InvoiceLine{{
		Description: plan.Name + " plan — " + itoa(mailboxes) + " mailbox(es) × R" + fmtPrice(perMailbox) + "/month",
		Quantity:    mailboxes,
		UnitZAR:     perMailbox,
	}}
	// Storage overage: recorded by the quota engine (owner decision — manual
	// invoicing), R10/GB beyond the included allowance.
	var overageGB int
	if err := s.Store.Pool.QueryRow(r.Context(),
		`select coalesce(storage_overage_gb,0) from mail.org where id=$1`, orgID).Scan(&overageGB); err == nil && overageGB > 0 {
		lines = append(lines, store.InvoiceLine{
			Description: itoa(overageGB) + " GB storage overage",
			Quantity:    overageGB,
			UnitZAR:     10,
		})
	}
	return lines, nil
}

func (s *Server) handleBillingSummary(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	sub, err := store.GetActiveSubscription(r.Context(), s.Store.Pool, ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	plan, err := store.GetPlanByID(r.Context(), s.Store.Pool, sub.PlanID)
	if err != nil {
		mapError(w, err)
		return
	}
	contact, err := store.GetBillingContact(r.Context(), s.Store.Pool, ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	methods, err := store.ListPaymentMethods(r.Context(), s.Store.Pool, ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	methodsOut := make([]map[string]any, 0, len(methods))
	for _, m := range methods {
		methodsOut = append(methodsOut, map[string]any{
			"id": ids.Format(ids.PaymentMethod, m.ID), "provider": m.Provider, "kind": m.Kind,
			"masked": m.Masked, "holder": m.Holder, "exp_month": m.ExpMonth, "exp_year": m.ExpYear,
			"is_default": m.IsDefault, "created_at": m.CreatedAt,
		})
	}
	planLine, _ := s.invoiceLineItems(r, ac.User.OrgID)
	writeJSON(w, http.StatusOK, map[string]any{
		"subscription": map[string]any{"status": sub.Status, "trial_ends_at": sub.TrialEndsAt, "current_period_end": sub.CurrentPeriodEnd},
		"plan": map[string]any{"slug": plan.Slug, "name": plan.Name, "price_cents": plan.PriceCents,
			"mailbox_limit": plan.MailboxLimit, "storage_total_mb": plan.StorageTotalMB},
		"contact":             contact,
		"payment_methods":     methodsOut,
		"paystack_configured": s.Cfg.Paystack.SecretKey != "",
		"next_invoice_lines":  planLine,
	})
}

func (s *Server) handleBillingContact(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	var req store.BillingContact
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	if req.BillingEmail == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "billing_email required")
		return
	}
	if err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.SetBillingContact(r.Context(), tx, ac.User.OrgID, &req)
	}); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contact": &req})
}

func (s *Server) handleBillingPreview(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	lines, err := s.invoiceLineItems(r, ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]store.InvoiceLine, 0, len(lines))
	var total float64
	for _, l := range lines {
		v := store.LineVAT(l.Quantity, l.UnitZAR)
		v.Description = l.Description
		out = append(out, v)
		total += v.TotalZAR
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": out, "vat_rate": store.VAT_RATE, "total_zar": math.Round(total*100) / 100})
}

func (s *Server) handleCreateInvoice(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if ac.User.Role != "owner" && ac.User.Role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	lines, err := s.invoiceLineItems(r, ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	var inv *store.Invoice
	if err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		var e error
		inv, e = store.CreateInvoice(r.Context(), tx, ac.User.OrgID, lines, 7*24*time.Hour)
		return e
	}); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"invoice": invoiceView(inv)})
}

func (s *Server) handleListInvoices(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	invoices, err := store.ListInvoices(r.Context(), s.Store.Pool, ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	payments, err := store.ListPayments(r.Context(), s.Store.Pool, ac.User.OrgID)
	if err != nil {
		mapError(w, err)
		return
	}
	paymentsOut := make([]map[string]any, 0, len(payments))
	for _, p := range payments {
		item := map[string]any{
			"id": ids.Format(ids.Payment, p.ID), "amount_zar": p.AmountZAR, "currency": p.Currency,
			"status": p.Status, "provider": p.Provider, "paid_at": p.PaidAt, "created_at": p.CreatedAt,
		}
		if p.InvoiceID != nil {
			item["invoice_id"] = ids.Format(ids.Invoice, *p.InvoiceID)
		}
		paymentsOut = append(paymentsOut, item)
	}
	out := make([]map[string]any, 0, len(invoices))
	for _, inv := range invoices {
		out = append(out, invoiceView(inv))
	}
	writeJSON(w, http.StatusOK, map[string]any{"invoices": out, "payments": paymentsOut})
}

func (s *Server) billingInvoice(r *http.Request) (*store.Invoice, bool) {
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Invoice)
	if !ok {
		return nil, false
	}
	ac := authFrom(r.Context())
	inv, err := store.GetInvoice(r.Context(), s.Store.Pool, id)
	if err != nil || inv.OrgID != ac.User.OrgID {
		return nil, false
	}
	return inv, true
}

func (s *Server) handleGetInvoice(w http.ResponseWriter, r *http.Request) {
	inv, ok := s.billingInvoice(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "invoice not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invoice": invoiceView(inv)})
}

// handlePayInvoice: initialises a Paystack hosted checkout for the invoice
// total (VAT-inclusive). Without a secret key the answer is honest: the
// invoice stays open, the client shows "charging not configured".
func (s *Server) handlePayInvoice(w http.ResponseWriter, r *http.Request) {
	inv, ok := s.billingInvoice(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "invoice not found")
		return
	}
	if inv.Status != "open" {
		writeError(w, http.StatusConflict, "conflict", "invoice is not open")
		return
	}
	if s.Cfg.Paystack.SecretKey == "" {
		writeErrorHint(w, http.StatusNotImplemented, "not_configured",
			"Card charging is not configured on this deployment",
			"set SYNKRA_MAIL_PAYSTACK_SECRET_KEY; the invoice stays open in the meantime")
		return
	}
	ac := authFrom(r.Context())
	contact, _ := store.GetBillingContact(r.Context(), s.Store.Pool, ac.User.OrgID)
	email := contact.BillingEmail
	if email == "" {
		email = ac.User.Email
	}
	ref := "SM-" + inv.Number + "-" + time.Now().UTC().Format("20060102150405")
	url, err := paystack.New(s.Cfg.Paystack.SecretKey).Initialise(r.Context(), email,
		int64(math.Round(inv.Total*100)), "ZAR", ref, "")
	if err != nil {
		mapError(w, err)
		return
	}
	if err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.SetInvoicePayURL(r.Context(), tx, inv.ID, ref, url); err != nil {
			return err
		}
		return store.RecordPayment(r.Context(), tx, &store.Payment{
			OrgID: ac.User.OrgID, InvoiceID: &inv.ID, AmountZAR: inv.Total, Currency: "ZAR",
			Status: "pending", Provider: "paystack", ProviderRef: &ref,
		})
	}); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pay_url": url, "reference": ref})
}

// handleVerifyInvoice resolves the checkout: Paystack verify → invoice paid +
// payment row success + card saved as a reusable payment method.
func (s *Server) handleVerifyInvoice(w http.ResponseWriter, r *http.Request) {
	inv, ok := s.billingInvoice(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "invoice not found")
		return
	}
	if s.Cfg.Paystack.SecretKey == "" {
		writeErrorHint(w, http.StatusNotImplemented, "not_configured", "Card charging is not configured", "")
		return
	}
	if inv.ProviderRef == nil {
		writeError(w, http.StatusConflict, "conflict", "no checkout started for this invoice")
		return
	}
	ac := authFrom(r.Context())
	t, err := paystack.New(s.Cfg.Paystack.SecretKey).Verify(r.Context(), *inv.ProviderRef)
	if err != nil {
		mapError(w, err)
		return
	}
	if t.Status != "success" {
		writeJSON(w, http.StatusOK, map[string]any{"verified": false, "status": t.Status})
		return
	}
	if err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.MarkInvoicePaid(r.Context(), tx, inv.ID, t.Reference); err != nil {
			return err
		}
		if err := store.RecordPayment(r.Context(), tx, &store.Payment{
			OrgID: ac.User.OrgID, InvoiceID: &inv.ID, AmountZAR: inv.Total, Currency: "ZAR",
			Status: "success", Provider: "paystack", ProviderRef: &t.Reference,
			PaidAt: ptrTime(time.Now().UTC()),
		}); err != nil {
			return err
		}
		if t.Authorization.Last4 != "" {
			last4 := t.Authorization.Last4
			holder := t.Authorization.Bank
			kind := "card"
			if t.Authorization.CardType != "" {
				kind = t.Authorization.CardType
			}
			return store.CreatePaymentMethod(r.Context(), tx, &store.PaymentMethod{
				OrgID: ac.User.OrgID, Provider: "paystack", Kind: kind,
				Masked: "•••• " + last4, Holder: &holder,
				ExpMonth: &t.Authorization.ExpMonth, ExpYear: &t.Authorization.ExpYear,
				IsDefault: true,
			})
		}
		return nil
	}); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"verified": true, "status": "paid"})
}

// handleMarkInvoicePaid: manual/offline capture — dev + demo path (prod
// capture flows through Paystack verify / webhooks).
func (s *Server) handleMarkInvoicePaid(w http.ResponseWriter, r *http.Request) {
	inv, ok := s.billingInvoice(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "invoice not found")
		return
	}
	if s.Cfg.Env != "dev" {
		writeError(w, http.StatusForbidden, "forbidden", "manual capture is dev-only")
		return
	}
	ac := authFrom(r.Context())
	ref := "MANUAL-" + time.Now().UTC().Format("20060102150405")
	if err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.MarkInvoicePaid(r.Context(), tx, inv.ID, ref); err != nil {
			return err
		}
		return store.RecordPayment(r.Context(), tx, &store.Payment{
			OrgID: ac.User.OrgID, InvoiceID: &inv.ID, AmountZAR: inv.Total, Currency: "ZAR",
			Status: "success", Provider: "manual", ProviderRef: &ref,
			PaidAt: ptrTime(time.Now().UTC()),
		})
	}); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "paid"})
}

func (s *Server) handleDeletePaymentMethod(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.PaymentMethod)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad id")
		return
	}
	if err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.DeletePaymentMethod(r.Context(), tx, ac.User.OrgID, id)
	}); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func invoiceView(inv *store.Invoice) map[string]any {
	return map[string]any{
		"id": ids.Format(ids.Invoice, inv.ID), "number": inv.Number, "status": inv.Status,
		"currency": inv.Currency, "subtotal_zar": inv.Subtotal, "vat_zar": inv.VAT,
		"total_zar": inv.Total, "issued_at": inv.IssuedAt, "due_at": inv.DueAt,
		"paid_at": inv.PaidAt, "provider": inv.Provider, "pay_url": inv.PayURL, "lines": inv.Lines,
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func fmtPrice(v float64) string {
	b, _ := json.Marshal(math.Round(v*100) / 100)
	return string(b)
}

func ptrTime(t time.Time) *time.Time { return &t }
