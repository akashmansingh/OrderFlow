package e2e

import (
	"io"
	"net/http"
	"testing"
	"time"
)

// These tests need the stack started with PAYMENT_MODE=checkout. Against an automatic-mode
// stack they skip, because reservations there carry no payment object and settle on their own.

type paymentInfo struct {
	Status        string `json:"status"`
	Attempts      int    `json:"attempts"`
	PaymentID     string `json:"payment_id"`
	FailureReason string `json:"failure_reason"`
}

type payableReservation struct {
	reservation
	Payment *paymentInfo `json:"payment"`
}

func requireCheckoutMode(t *testing.T, created payableReservation) {
	t.Helper()
	if created.Payment == nil {
		t.Skip("stack is not in checkout mode; start it with PAYMENT_MODE=checkout docker compose up -d --build --wait")
	}
}

func createPayableReservation(t *testing.T, accessToken, eventID string, quantity int) payableReservation {
	t.Helper()

	response := authenticatedRequest(t, accessToken, http.MethodPost, "/v1/events/"+eventID+"/reservations", map[string]any{
		"quantity": quantity,
	}, map[string]string{
		"Idempotency-Key": randomUUID(t),
	})
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("create reservation status = %d, want %d; body = %s", response.StatusCode, http.StatusCreated, payload)
	}

	var created payableReservation
	decodeJSON(t, response.Body, &created)
	return created
}

func pay(t *testing.T, accessToken, reservationID, scenario, idempotencyKey string) (int, payableReservation) {
	t.Helper()

	response := authenticatedRequest(t, accessToken, http.MethodPost, "/v1/reservations/"+reservationID+"/payment", map[string]any{
		"scenario": scenario,
	}, map[string]string{
		"Idempotency-Key": idempotencyKey,
	})
	defer response.Body.Close()

	var result payableReservation
	if response.StatusCode == http.StatusAccepted {
		decodeJSON(t, response.Body, &result)
	} else {
		_, _ = io.Copy(io.Discard, response.Body)
	}
	return response.StatusCode, result
}

func getPayable(t *testing.T, accessToken, reservationID string) payableReservation {
	t.Helper()

	response := authenticatedRequest(t, accessToken, http.MethodGet, "/v1/reservations/"+reservationID, nil, nil)
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("get reservation status = %d, want %d; body = %s", response.StatusCode, http.StatusOK, payload)
	}

	var current payableReservation
	decodeJSON(t, response.Body, &current)
	return current
}

// waitForPayment polls until the payment reaches wanted or the deadline passes.
func waitForPayment(t *testing.T, accessToken, reservationID, wanted string) payableReservation {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for {
		current := getPayable(t, accessToken, reservationID)
		if current.Payment != nil && current.Payment.Status == wanted {
			return current
		}
		if time.Now().After(deadline) {
			status := "<none>"
			if current.Payment != nil {
				status = current.Payment.Status
			}
			t.Fatalf("payment status = %q after 30s, want %q (reservation %q)", status, wanted, current.Status)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func TestPayNowConfirmsHeldReservation(t *testing.T) {
	requireE2E(t)

	_, token := registerAndLogin(t)
	createdEvent := createEvent(t, token.AccessToken, 5)
	held := createPayableReservation(t, token.AccessToken, createdEvent.ID, 2)
	requireCheckoutMode(t, held)

	if held.Status != "held" || held.Payment.Status != "awaiting" {
		t.Fatalf("new reservation = %s / payment %s, want held / awaiting", held.Status, held.Payment.Status)
	}

	key := randomUUID(t)
	status, accepted := pay(t, token.AccessToken, held.ID, "approve", key)
	if status != http.StatusAccepted {
		t.Fatalf("pay status = %d, want %d", status, http.StatusAccepted)
	}
	if accepted.Status != "held" {
		t.Fatalf("reservation status right after pay = %q, want held until the worker settles it", accepted.Status)
	}

	// Replaying the same key must answer, not start a second attempt.
	replayStatus, _ := pay(t, token.AccessToken, held.ID, "approve", key)
	if replayStatus != http.StatusAccepted {
		t.Fatalf("replay status = %d, want %d", replayStatus, http.StatusAccepted)
	}

	settled := waitForPayment(t, token.AccessToken, held.ID, "succeeded")
	if settled.Status != "confirmed" {
		t.Fatalf("reservation status = %q, want confirmed", settled.Status)
	}
	if settled.Payment.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (the replay must not create another)", settled.Payment.Attempts)
	}

	// A confirmed reservation can no longer be paid.
	again, _ := pay(t, token.AccessToken, held.ID, "approve", randomUUID(t))
	if again != http.StatusConflict {
		t.Fatalf("pay on confirmed reservation status = %d, want %d", again, http.StatusConflict)
	}
}

func TestDeclinedPaymentKeepsHoldOpenForRetry(t *testing.T) {
	requireE2E(t)

	_, token := registerAndLogin(t)
	createdEvent := createEvent(t, token.AccessToken, 5)
	held := createPayableReservation(t, token.AccessToken, createdEvent.ID, 1)
	requireCheckoutMode(t, held)

	if status, _ := pay(t, token.AccessToken, held.ID, "decline", randomUUID(t)); status != http.StatusAccepted {
		t.Fatalf("pay status = %d, want %d", status, http.StatusAccepted)
	}

	declined := waitForPayment(t, token.AccessToken, held.ID, "declined")
	if declined.Status != "held" {
		t.Fatalf("reservation status after decline = %q, want held so the customer can retry", declined.Status)
	}
	if declined.Payment.FailureReason != "card_declined" {
		t.Fatalf("failure reason = %q, want card_declined", declined.Payment.FailureReason)
	}

	if status, _ := pay(t, token.AccessToken, held.ID, "approve", randomUUID(t)); status != http.StatusAccepted {
		t.Fatalf("retry status = %d, want %d", status, http.StatusAccepted)
	}

	settled := waitForPayment(t, token.AccessToken, held.ID, "succeeded")
	if settled.Status != "confirmed" {
		t.Fatalf("reservation status = %q, want confirmed after the retry", settled.Status)
	}
	if settled.Payment.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", settled.Payment.Attempts)
	}
}

func TestPaymentIsOwnedByReservationCustomer(t *testing.T) {
	requireE2E(t)

	_, owner := registerAndLogin(t)
	_, stranger := registerAndLogin(t)
	createdEvent := createEvent(t, owner.AccessToken, 5)
	held := createPayableReservation(t, owner.AccessToken, createdEvent.ID, 1)
	requireCheckoutMode(t, held)

	if status, _ := pay(t, stranger.AccessToken, held.ID, "approve", randomUUID(t)); status != http.StatusNotFound {
		t.Fatalf("pay by another customer status = %d, want %d", status, http.StatusNotFound)
	}
}

func TestPaymentRejectsUnknownScenario(t *testing.T) {
	requireE2E(t)

	_, token := registerAndLogin(t)
	createdEvent := createEvent(t, token.AccessToken, 5)
	held := createPayableReservation(t, token.AccessToken, createdEvent.ID, 1)
	requireCheckoutMode(t, held)

	if status, _ := pay(t, token.AccessToken, held.ID, "4242424242424242", randomUUID(t)); status != http.StatusBadRequest {
		t.Fatalf("pay with a card number as scenario status = %d, want %d", status, http.StatusBadRequest)
	}
}
