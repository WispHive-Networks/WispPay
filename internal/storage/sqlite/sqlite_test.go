package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	mysqlstore "github.com/WispHive-Networks/WispPay/internal/storage/mysql"
)

func TestSQLitePersistsWorkspaceAndSessions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "wisppay.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateBusinessUser(ctx, "Example Business", "owner@example.com", "password-hash"); err != nil {
		t.Fatal(err)
	}
	user, err := db.FindUserByEmail(ctx, "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSession(ctx, "session-hash", "csrf-hash", user.ID, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	category, err := db.CreateCategory(ctx, user.BusinessID, "Services")
	if err != nil {
		t.Fatal(err)
	}
	categoryID := category.ID
	product, err := db.CreateProduct(ctx, user.BusinessID, mysqlstore.Product{
		CategoryID: &categoryID, Name: "Consultation", Description: "One hour", Price: "25.00", Currency: "USD", Available: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := db.CreateClient(ctx, user.BusinessID, mysqlstore.Client{DisplayName: "Local Client"})
	if err != nil {
		t.Fatal(err)
	}
	invoice, err := db.CreateInvoice(ctx, user.BusinessID, client.ID, "http://localhost:18080", []mysqlstore.InvoiceItemInput{{ProductID: product.ID, Quantity: 2}})
	if err != nil {
		t.Fatal(err)
	}
	loadedInvoice, err := db.GetInvoice(ctx, user.BusinessID, invoice.ID)
	if err != nil || loadedInvoice.Total != "50.00" || len(loadedInvoice.Items) != 1 {
		t.Fatalf("invoice detail = %+v, err = %v", loadedInvoice, err)
	}
	invoices, err := db.ListInvoices(ctx, user.BusinessID)
	if err != nil || len(invoices) != 1 || invoices[0].ClientName != client.DisplayName {
		t.Fatalf("invoice list = %+v, err = %v", invoices, err)
	}
	publicInvoice, err := db.GetPublicInvoice(ctx, invoice.PublicToken)
	if err != nil {
		t.Fatal(err)
	}
	if publicInvoice.Total != "50.00" || len(publicInvoice.Items) != 1 {
		t.Fatalf("public invoice = %+v", publicInvoice)
	}
	if err := db.BeginPayPalSellerOnboarding(ctx, user.BusinessID, "onboarding-state"); err != nil {
		t.Fatal(err)
	}
	if err := db.CompletePayPalSellerOnboarding(ctx, user.BusinessID, "onboarding-state", "sandbox-merchant", true, true, true); err != nil {
		t.Fatal(err)
	}
	payment, err := db.StartPayPalPayment(ctx, invoice.PublicToken)
	if err != nil {
		t.Fatal(err)
	}
	payment.OrderID = "sandbox-order"
	payment.ApprovalURL = "https://www.sandbox.paypal.com/checkoutnow?token=sandbox-order"
	if err := db.SavePayPalOrder(ctx, payment); err != nil {
		t.Fatal(err)
	}
	reserved, err := db.ReservePayPalCapture(ctx, invoice.PublicToken, payment.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CompletePayPalCapture(ctx, reserved, payment.OrderID, "sandbox-capture", "USD", "50.00"); err != nil {
		t.Fatal(err)
	}
	paidInvoice, err := db.GetInvoice(ctx, user.BusinessID, invoice.ID)
	if err != nil || paidInvoice.Status != "paid" {
		t.Fatalf("paid invoice = %+v, err = %v", paidInvoice, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	loaded, err := db.FindUserByEmail(ctx, "owner@example.com")
	if err != nil || loaded.ID != user.ID {
		t.Fatalf("persisted user = %+v, err = %v", loaded, err)
	}
	session, err := db.FindSession(ctx, "session-hash")
	if err != nil || session.ID != user.ID {
		t.Fatalf("persisted session = %+v, err = %v", session, err)
	}
}
