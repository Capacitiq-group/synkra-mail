// billing.go — the billing document (phase 11): invoice generation with
// per-line 15% VAT, payment methods, payment history, org billing contact.
// Charging runs through Paystack; when no secret key is configured the
// invoice stays open and the pay endpoint says so honestly.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const VAT_RATE = 0.15

// InvoiceLine is one invoice row. VAT is computed per line and stored.
type InvoiceLine struct {
	Description string  `json:"description"`
	Quantity    int     `json:"quantity"`
	UnitZAR     float64 `json:"unit_zar"`
	VATZAR      float64 `json:"vat_zar"`
	TotalZAR    float64 `json:"total_zar"`
}

// LineVAT computes the 15% VAT and total for a line (ex-VAT unit price).
func LineVAT(qty int, unit float64) InvoiceLine {
	sub := math.Round(float64(qty)*unit*100) / 100
	vat := math.Round(sub*VAT_RATE*100) / 100
	return InvoiceLine{Quantity: qty, UnitZAR: unit, VATZAR: vat, TotalZAR: math.Round((sub+vat)*100) / 100}
}

type Invoice struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	Number      string
	Status      string // open | paid | void
	Currency    string
	Subtotal    float64
	VAT         float64
	Total       float64
	IssuedAt    time.Time
	DueAt       *time.Time
	PaidAt      *time.Time
	Provider    string
	ProviderRef *string
	PayURL      *string
	Lines       []InvoiceLine
}

const invoiceSelect = `select id, org_id, number, status, currency, subtotal_zar, vat_zar, total_zar,
	issued_at, due_at, paid_at, provider, provider_ref, pay_url, lines from mail.invoice`

func scanInvoice(row pgx.Row) (*Invoice, error) {
	var inv Invoice
	var lines []byte
	err := row.Scan(&inv.ID, &inv.OrgID, &inv.Number, &inv.Status, &inv.Currency, &inv.Subtotal, &inv.VAT, &inv.Total,
		&inv.IssuedAt, &inv.DueAt, &inv.PaidAt, &inv.Provider, &inv.ProviderRef, &inv.PayURL, &lines)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(lines, &inv.Lines)
	return &inv, nil
}

func GetInvoice(ctx context.Context, q Querier, id uuid.UUID) (*Invoice, error) {
	return scanInvoice(q.QueryRow(ctx, invoiceSelect+` where id=$1`, id))
}

func ListInvoices(ctx context.Context, q Querier, orgID uuid.UUID) ([]*Invoice, error) {
	rows, err := q.Query(ctx, invoiceSelect+` where org_id=$1 order by issued_at desc limit 100`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Invoice
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// NextInvoiceNumber: SM-YYYY-####, per-year sequence.
func NextInvoiceNumber(ctx context.Context, tx pgx.Tx) (string, error) {
	year := time.Now().UTC().Year()
	var seq int
	err := tx.QueryRow(ctx, `select count(*)+1 from mail.invoice where number like $1`,
		fmt.Sprintf("SM-%d-%%", year)).Scan(&seq)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("SM-%d-%04d", year, seq), nil
}

// CreateInvoice inserts an invoice with computed per-line VAT. Lines arrive
// ex-VAT; totals are derived here (never trusted from the caller).
func CreateInvoice(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, lines []InvoiceLine, dueIn time.Duration) (*Invoice, error) {
	var subtotal, vat, total float64
	for i := range lines {
		l := LineVAT(lines[i].Quantity, lines[i].UnitZAR)
		l.Description = lines[i].Description
		lines[i] = l
		sub := math.Round(float64(l.Quantity)*l.UnitZAR*100) / 100
		subtotal += sub
		vat += l.VATZAR
		total += l.TotalZAR
	}
	subtotal = math.Round(subtotal*100) / 100
	vat = math.Round(vat*100) / 100
	total = math.Round(total*100) / 100
	num, err := NextInvoiceNumber(ctx, tx)
	if err != nil {
		return nil, err
	}
	linesJSON, _ := json.Marshal(lines)
	due := time.Now().Add(dueIn)
	id := uuid.New()
	_, err = tx.Exec(ctx, `insert into mail.invoice (id, org_id, number, status, currency, subtotal_zar, vat_zar, total_zar, due_at, lines)
		values ($1,$2,$3,'open','ZAR',$4,$5,$6,$7,$8)`, id, orgID, num, subtotal, vat, total, due, linesJSON)
	if err != nil {
		return nil, err
	}
	return GetInvoice(ctx, tx, id)
}

func MarkInvoicePaid(ctx context.Context, tx pgx.Tx, invoiceID uuid.UUID, providerRef string) error {
	_, err := tx.Exec(ctx, `update mail.invoice set status='paid', paid_at=now(), provider_ref=$2 where id=$1 and status='open'`,
		invoiceID, providerRef)
	return err
}

func SetInvoicePayURL(ctx context.Context, tx pgx.Tx, invoiceID uuid.UUID, ref, url string) error {
	_, err := tx.Exec(ctx, `update mail.invoice set provider_ref=$2, pay_url=$3 where id=$1`, invoiceID, ref, url)
	return err
}

// ---- payment methods ----

type PaymentMethod struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	Provider  string
	Kind      string
	Masked    string
	Holder    *string
	ExpMonth  *int
	ExpYear   *int
	IsDefault bool
	CreatedAt time.Time
}

func ListPaymentMethods(ctx context.Context, q Querier, orgID uuid.UUID) ([]PaymentMethod, error) {
	rows, err := q.Query(ctx, `select id, org_id, provider, kind, masked, holder, exp_month, exp_year, is_default, created_at
		from mail.payment_method where org_id=$1 order by is_default desc, created_at desc`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PaymentMethod
	for rows.Next() {
		var m PaymentMethod
		if err := rows.Scan(&m.ID, &m.OrgID, &m.Provider, &m.Kind, &m.Masked, &m.Holder, &m.ExpMonth, &m.ExpYear, &m.IsDefault, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func CreatePaymentMethod(ctx context.Context, tx pgx.Tx, m *PaymentMethod) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	_, err := tx.Exec(ctx, `insert into mail.payment_method (id, org_id, provider, kind, masked, holder, exp_month, exp_year, is_default)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		m.ID, m.OrgID, m.Provider, m.Kind, m.Masked, m.Holder, m.ExpMonth, m.ExpYear, m.IsDefault)
	return err
}

func DeletePaymentMethod(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID) error {
	_, err := tx.Exec(ctx, `delete from mail.payment_method where id=$1 and org_id=$2`, id, orgID)
	return err
}

// ---- payment history ----

type Payment struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	InvoiceID   *uuid.UUID
	AmountZAR   float64
	Currency    string
	Status      string
	Provider    string
	ProviderRef *string
	PaidAt      *time.Time
	CreatedAt   time.Time
}

func RecordPayment(ctx context.Context, tx pgx.Tx, p *Payment) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	_, err := tx.Exec(ctx, `insert into mail.payment (id, org_id, invoice_id, amount_zar, currency, status, provider, provider_ref, paid_at)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		p.ID, p.OrgID, p.InvoiceID, p.AmountZAR, p.Currency, p.Status, p.Provider, p.ProviderRef, p.PaidAt)
	return err
}

func ListPayments(ctx context.Context, q Querier, orgID uuid.UUID) ([]Payment, error) {
	rows, err := q.Query(ctx, `select id, org_id, invoice_id, amount_zar, currency, status, provider, provider_ref, paid_at, created_at
		from mail.payment where org_id=$1 order by created_at desc limit 100`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Payment
	for rows.Next() {
		var p Payment
		if err := rows.Scan(&p.ID, &p.OrgID, &p.InvoiceID, &p.AmountZAR, &p.Currency, &p.Status, &p.Provider, &p.ProviderRef, &p.PaidAt, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- billing contact ----

type BillingContact struct {
	BillingEmail       string `json:"billing_email"`
	BillingContactName string `json:"billing_contact_name"`
	VATNumber          string `json:"vat_number"`
}

func GetBillingContact(ctx context.Context, q Querier, orgID uuid.UUID) (*BillingContact, error) {
	b := &BillingContact{}
	err := q.QueryRow(ctx, `select coalesce(billing_email,''), coalesce(billing_contact_name,''), coalesce(vat_number,'') from mail.org where id=$1`, orgID).
		Scan(&b.BillingEmail, &b.BillingContactName, &b.VATNumber)
	return b, err
}

func SetBillingContact(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, b *BillingContact) error {
	_, err := tx.Exec(ctx, `update mail.org set billing_email=$2, billing_contact_name=$3, vat_number=$4 where id=$1`,
		orgID, b.BillingEmail, b.BillingContactName, b.VATNumber)
	return err
}

// CountOrgMailboxes: the plan line's quantity basis.
func CountOrgMailboxes(ctx context.Context, q Querier, orgID uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from mail.mailbox where org_id=$1`, orgID).Scan(&n)
	return n, err
}
