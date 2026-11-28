// handlers_domainreg.go — registrar-managed domain lifecycle (phase 11):
// add-domain wizard (Oner registration + authoritative zone creation),
// nameservers/transfer-lock/WHOIS-privacy/auto-renew, EPP retrieval behind a
// password step-up, registrant contacts, status sync, renew, and the DNS
// doctor (expected records + live verdicts from dnssvc.Doctor).
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"synkra.email/mail/internal/dnssvc"
	"synkra.email/mail/internal/ids"
	"synkra.email/mail/internal/krypto"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/registrar"
	"synkra.email/mail/internal/store"
)

// reg builds the reseller client per request. No API key → dry-run in dev
// (prod config validation refuses dry-run).
func (s *Server) reg() registrar.Registrar {
	if s.Cfg.Registrar.APIKey != "" {
		return registrar.NewOner(s.Cfg.Registrar.BaseURL, s.Cfg.Registrar.APIKey)
	}
	return registrar.DryRun{}
}

func (s *Server) domainAccess(r *http.Request, idParam string) (*model.Domain, *store.RegistrarInfo, bool) {
	id, ok := parseIDParam(idParam, ids.Domain)
	if !ok {
		return nil, nil, false
	}
	ac := authFrom(r.Context())
	dom, err := store.GetDomain(r.Context(), s.Store.Pool, id)
	if err != nil || (dom.OrgID != nil && *dom.OrgID != ac.User.OrgID && !ac.User.IsPlatformAdmin) {
		return nil, nil, false
	}
	info, err := store.GetRegistrarInfo(r.Context(), s.Store.Pool, id)
	if err != nil {
		return nil, nil, false
	}
	return dom, info, true
}

// ---- wizard: availability + registration ----

func (s *Server) handleDomainCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Names []string `json:"names"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	if len(req.Names) == 0 || len(req.Names) > 20 {
		writeError(w, http.StatusBadRequest, "invalid_request", "1..20 names")
		return
	}
	for i, n := range req.Names {
		n = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(n), "."))
		if !store.ValidDomainName(n) {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid domain: "+n)
			return
		}
		req.Names[i] = n
	}
	checks, err := s.reg().CheckAvailable(r.Context(), req.Names)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": checks})
}

// handleRegisterDomain: the wizard's confirm step. Registers through the
// reseller, provisions the authoritative zone (mail-ready template) when the
// customer delegates to us, and stages the domain row with its ZARC state.
func (s *Server) handleRegisterDomain(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	if ac.User.Role != "owner" && ac.User.Role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "org admin only")
		return
	}
	var req struct {
		Name       string                      `json:"name"`
		Years      int                         `json:"years"`
		Registrant registrar.RegistrantContact `json:"registrant"`
		NSMode     string                      `json:"ns_mode"` // synkra | custom
		CustomNS   []string                    `json:"custom_nameservers"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(req.Name), "."))
	if !store.ValidDomainName(name) {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid domain name")
		return
	}
	if req.Years < 1 || req.Years > 5 {
		writeError(w, http.StatusBadRequest, "invalid_request", "years must be 1..5")
		return
	}
	if req.NSMode != "synkra" && req.NSMode != "custom" {
		writeError(w, http.StatusBadRequest, "invalid_request", "ns_mode must be synkra|custom")
		return
	}
	if req.NSMode == "custom" && len(req.CustomNS) < 2 {
		writeError(w, http.StatusBadRequest, "invalid_request", "custom delegation needs at least two nameservers")
		return
	}

	// Idempotency: a live row with this name already belongs to an org.
	if _, err := store.GetDomainByName(r.Context(), s.Store.Pool, name); err == nil {
		writeError(w, http.StatusConflict, "conflict", "domain already present")
		return
	}

	ns := []string{"ns1." + s.Cfg.Hostname, "ns2." + s.Cfg.Hostname}
	managed := "full"
	if req.NSMode == "custom" {
		ns = req.CustomNS
		managed = "partial"
	}

	info, err := s.reg().Register(r.Context(), name, req.Years, req.Registrant, ns)
	if err != nil {
		mapError(w, err)
		return
	}

	// Domain row: reuse the phase-8 creation invariants (cap, keys, audit),
	// then overlay the registrar columns.
	d := &model.Domain{
		ID:             uuid.New(),
		OrgID:          &ac.User.OrgID,
		Name:           name,
		Status:         "pending",
		OwnershipToken: "synkra-verify-" + uuid.NewString()[:8],
	}
	if err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.CreateDomain(r.Context(), tx, d)
	}); err != nil {
		mapError(w, err)
		return
	}

	registrantJSON, _ := json.Marshal(req.Registrant)
	infoPatch := &store.RegistrarInfo{
		Registrar:      "oner",
		RegistryStatus: info.RegistryStatus,
		Lifecycle:      registrar.DeriveLifecycle(time.Now(), info.RenewsUntil, info.RegistryStatus),
		RegisteredAt:   info.RegisteredAt,
		RenewsUntil:    info.RenewsUntil,
		AutoRenew:      true,
		Nameservers:    ns,
		ManagedMode:    managed,
		Registrant:     registrantJSON,
	}
	if err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.UpdateRegistrarInfo(r.Context(), tx, d.ID, infoPatch); err != nil {
			return err
		}
		return store.RecordAudit(r.Context(), tx, &ac.User.ID, nil, false, "domain.register", "domain", d.ID.String(), name, jsonBytes(map[string]any{"years": req.Years, "ns_mode": req.NSMode}))
	}); err != nil {
		mapError(w, err)
		return
	}

	// Authoritative zone: created BEFORE delegation when we manage the zone
	// (register §4b: createZone before delegation — the zero-cache path).
	zoneProvisioned := false
	zoneErr := ""
	if managed == "full" {
		dnss := dnssvc.NewPG(s.Store, &ac.User.ID)
		in := dnssvc.TemplateInput{
			Domain: name, DKIMSelector: d.DKIMSelector, DKIMRecordTXT: d.DKIMPublicTXT,
			Hostname: s.Cfg.Hostname, PlatformDomain: s.Cfg.PlatformDomain,
			OwnershipToken: d.OwnershipToken, HostIP: "",
		}
		if err := dnss.EnsureMailReadyZone(r.Context(), in); err != nil {
			zoneErr = err.Error() // non-fatal: the doctor offers "provision zone"
		} else {
			zoneProvisioned = true
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"domain": d.Name, "domain_id": ids.Format(ids.Domain, d.ID),
		"registrar": infoPatch, "zone_provisioned": zoneProvisioned, "zone_error": zoneErr,
	})
}

// ---- per-domain detail ----

func (s *Server) handleDomainDetail(w http.ResponseWriter, r *http.Request) {
	dom, info, ok := s.domainAccess(r, chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	if info.RenewsUntil != nil {
		info.Lifecycle = registrar.DeriveLifecycle(time.Now(), info.RenewsUntil, info.RegistryStatus)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"domain": map[string]any{
			"id":            ids.Format(ids.Domain, dom.ID),
			"name":          dom.Name,
			"status":        dom.Status,
			"dkim_selector": dom.DKIMSelector,
			"verified_at":   dom.VerifiedAt,
			"expires_at":    dom.ExpiresAt,
		},
		"registrar":         info,
		"grace_note":        registrar.GraceWindow(info.Lifecycle),
		"partially_managed": info.ManagedMode == "partial",
	})
}

// ---- nameservers / locks / privacy / auto-renew / registrant ----

func (s *Server) handleDomainNameservers(w http.ResponseWriter, r *http.Request) {
	dom, info, ok := s.domainAccess(r, chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	var req struct {
		Nameservers []string `json:"nameservers"`
		Mode        string   `json:"mode"` // synkra | custom (derived when absent)
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	ns := req.Nameservers
	mode := req.Mode
	if mode == "" {
		if len(ns) >= 2 && strings.Contains(strings.Join(ns, ","), s.Cfg.Hostname) {
			mode = "synkra"
		} else if len(ns) >= 2 {
			mode = "custom"
		} else {
			writeError(w, http.StatusBadRequest, "invalid_request", "at least two nameservers")
			return
		}
	}
	if mode == "synkra" {
		ns = []string{"ns1." + s.Cfg.Hostname, "ns2." + s.Cfg.Hostname}
	}
	if err := s.reg().SetNameservers(r.Context(), dom.Name, ns); err != nil {
		mapError(w, err)
		return
	}
	managed := "full"
	if mode == "custom" {
		managed = "partial"
	}
	if err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.SetNameserversDB(r.Context(), tx, dom.ID, ns, managed)
	}); err != nil {
		mapError(w, err)
		return
	}
	info.Nameservers = ns
	info.ManagedMode = managed
	writeJSON(w, http.StatusOK, map[string]any{"nameservers": ns, "managed_mode": managed})
}

// domainBoolAction is the shared toggle body for transfer-lock / WHOIS
// privacy / auto-renew: registrar call first, DB row after, audited.
func (s *Server) domainBoolAction(w http.ResponseWriter, r *http.Request, column, audit string, call func(ctx context.Context, name string, enabled bool) error) {
	dom, _, ok := s.domainAccess(r, chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	if err := call(r.Context(), dom.Name, req.Enabled); err != nil {
		mapError(w, err)
		return
	}
	ac := authFrom(r.Context())
	if err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if _, e := tx.Exec(r.Context(), `update mail.domain set `+column+`=$2, updated_at=now() where id=$1`, dom.ID, req.Enabled); e != nil {
			return e
		}
		return store.RecordAudit(r.Context(), tx, &ac.User.ID, nil, false, audit, "domain", dom.ID.String(), dom.Name, jsonBytes(map[string]any{"enabled": req.Enabled}))
	}); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true, "enabled": req.Enabled})
}

func (s *Server) handleDomainTransferLock(w http.ResponseWriter, r *http.Request) {
	s.domainBoolAction(w, r, "transfer_lock", "domain.transfer_lock", s.reg().SetTransferLock)
}

func (s *Server) handleDomainWhoisPrivacy(w http.ResponseWriter, r *http.Request) {
	s.domainBoolAction(w, r, "whois_privacy", "domain.whois_privacy", s.reg().SetWhoisPrivacy)
}

func (s *Server) handleDomainAutoRenew(w http.ResponseWriter, r *http.Request) {
	s.domainBoolAction(w, r, "auto_renew", "domain.auto_renew", s.reg().SetAutoRenew)
}

func (s *Server) handleDomainRegistrant(w http.ResponseWriter, r *http.Request) {
	dom, _, ok := s.domainAccess(r, chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	var req registrar.RegistrantContact
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	if req.Email == "" || req.FirstName == "" || req.LastName == "" || req.CountryCode == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "registrant needs first/last name, email, country")
		return
	}
	if err := s.reg().SetRegistrant(r.Context(), dom.Name, req); err != nil {
		mapError(w, err)
		return
	}
	b, _ := json.Marshal(req)
	if err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		_, e := tx.Exec(r.Context(), `update mail.domain set registrant=$2, registry_status=case when registry_status='ok' then 'pending_update' else registry_status end, updated_at=now() where id=$1`, dom.ID, b)
		return e
	}); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "note": "registrant change sent to the registry — a contact-verification e-mail follows"})
}

// ---- EPP / transfer ----

// handleDomainEPP: transfer authorisation code retrieval. STEP-UP AUTH: the
// caller must re-confirm their password; the retrieval is audited (fail-closed
// pattern from the impersonation audit).
func (s *Server) handleDomainEPP(w http.ResponseWriter, r *http.Request) {
	dom, _, ok := s.domainAccess(r, chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	ac := authFrom(r.Context())
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "password confirmation required")
		return
	}
	user, err := store.GetUser(r.Context(), s.Store.Pool, ac.User.ID)
	if err != nil {
		mapError(w, err)
		return
	}
	if verr := krypto.VerifyPassword(user.PasswordHash, req.Password); verr != nil {
		writeError(w, http.StatusForbidden, "invalid_credentials", "password confirmation failed")
		return
	}
	code, err := s.reg().EPPCode(r.Context(), dom.Name)
	if err != nil {
		mapError(w, err)
		return
	}
	if err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.SetEPPRetrieved(r.Context(), tx, dom.ID); err != nil {
			return err
		}
		return store.RecordAudit(r.Context(), tx, &ac.User.ID, nil, false, "domain.epp_retrieved", "domain", dom.ID.String(), dom.Name, nil)
	}); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"epp_code": code})
}

func (s *Server) handleDomainTransferOut(w http.ResponseWriter, r *http.Request) {
	dom, _, ok := s.domainAccess(r, chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	var req struct {
		EPPCode string `json:"epp_code"`
	}
	if err := decodeJSON(r, &req); err != nil || req.EPPCode == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "epp_code required")
		return
	}
	if err := s.reg().TransferOut(r.Context(), dom.Name, req.EPPCode); err != nil {
		mapError(w, err)
		return
	}
	ac := authFrom(r.Context())
	if err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.RecordAudit(r.Context(), tx, &ac.User.ID, nil, false, "domain.transfer_out", "domain", dom.ID.String(), dom.Name, nil)
	}); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- renew / status sync ----

func (s *Server) handleDomainRenew(w http.ResponseWriter, r *http.Request) {
	dom, _, ok := s.domainAccess(r, chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	var req struct {
		Years int `json:"years"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad body")
		return
	}
	if req.Years < 1 || req.Years > 5 {
		writeError(w, http.StatusBadRequest, "invalid_request", "years must be 1..5")
		return
	}
	until, err := s.reg().Renew(r.Context(), dom.Name, req.Years)
	if err != nil {
		mapError(w, err)
		return
	}
	if err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		_, e := tx.Exec(r.Context(), `update mail.domain set renews_until=$2, lifecycle_state='renew_grace', updated_at=now() where id=$1`, dom.ID, until)
		return e
	}); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"renews_until": until, "lifecycle": "renew_grace"})
}

// handleDomainSync: pull the registrar's view (registry status, lifecycle,
// nameservers, locks) into the row — the dashboard's refresh action.
func (s *Server) handleDomainSync(w http.ResponseWriter, r *http.Request) {
	dom, _, ok := s.domainAccess(r, chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	info, err := s.reg().Info(r.Context(), dom.Name)
	if err != nil {
		mapError(w, err)
		return
	}
	patch := &store.RegistrarInfo{
		Registrar:      "oner",
		RegistryStatus: info.RegistryStatus,
		Lifecycle:      registrar.DeriveLifecycle(time.Now(), info.RenewsUntil, info.RegistryStatus),
		RegisteredAt:   info.RegisteredAt,
		RenewsUntil:    info.RenewsUntil,
		AutoRenew:      info.AutoRenew,
		TransferLock:   info.TransferLock,
		WhoisPrivacy:   info.WhoisPrivacy,
		Nameservers:    info.Nameservers,
		ManagedMode:    "full",
	}
	// Preserve locally-known fields the info payload may not carry.
	if cur, err := store.GetRegistrarInfo(r.Context(), s.Store.Pool, dom.ID); err == nil {
		patch.Registrant = cur.Registrant
		if cur.ManagedMode == "full" || cur.ManagedMode == "partial" || cur.ManagedMode == "unmanaged" {
			patch.ManagedMode = cur.ManagedMode
		}
		if len(patch.Nameservers) == 0 {
			patch.Nameservers = cur.Nameservers
		}
	}
	if err := s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		return store.UpdateRegistrarInfo(r.Context(), tx, dom.ID, patch)
	}); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"registrar": patch})
}

// handleDomainDoctor composes the phase-8 expected-records payload with LIVE
// verdicts from dnssvc.Doctor and the zone state.
func (s *Server) handleDomainDoctor(w http.ResponseWriter, r *http.Request) {
	dom, info, ok := s.domainAccess(r, chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "no access")
		return
	}
	expected := []map[string]string{
		{"type": "MX", "name": "@", "value": "10 " + s.Cfg.Hostname, "purpose": "receive"},
		{"type": "TXT", "name": "@",
			"value":   "v=spf1 include:spf." + s.Cfg.PlatformDomain + " ~all",
			"purpose": "SPF — merge include:spf." + s.Cfg.PlatformDomain + " into your existing v=spf1 record; never publish a second v=spf1 record"},
		{"type": "TXT", "name": dom.DKIMSelector + "._domainkey", "value": dom.DKIMPublicTXT, "purpose": "DKIM"},
		{"type": "TXT", "name": "_dmarc",
			"value":   "v=DMARC1; p=quarantine; rua=mailto:dmarc-reports@" + s.Cfg.PlatformDomain,
			"purpose": "DMARC policy — report address is authorised by the platform automatically; nothing to publish for it"},
		{"type": "TXT", "name": "@", "value": dom.OwnershipToken, "purpose": "ownership verification"},
	}
	in := dnssvc.TemplateInput{
		Domain: dom.Name, DKIMSelector: dom.DKIMSelector, DKIMRecordTXT: dom.DKIMPublicTXT,
		Hostname: s.Cfg.Hostname, PlatformDomain: s.Cfg.PlatformDomain,
		OwnershipToken: dom.OwnershipToken, HostIP: "",
	}
	// Live lookups through the system resolver. In a sandbox without
	// recursion the checks come back "unverifiable" (empty actual) — honest,
	// never pass-by-default.
	checks := dnssvc.Doctor(r.Context(), in, nil)
	checksOut := make([]map[string]any, 0, len(checks))
	for _, c := range checks {
		verdict := "fail"
		if c.Pass {
			verdict = "pass"
		} else if c.Actual == "" {
			verdict = "unverifiable"
		}
		checksOut = append(checksOut, map[string]any{
			"record": c.Record, "expected": c.Expected, "actual": c.Actual,
			"verdict": verdict, "fix": c.Fix,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"domain":     dom.Name,
		"records":    expected,
		"checks":     checksOut,
		"zone":       map[string]any{"managed": info.ManagedMode, "nameservers": info.Nameservers},
		"checked_at": time.Now().UTC(),
	})
}

func jsonBytes(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
