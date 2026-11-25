package workflow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/theRiddler9/OrderFlow/internal/config"
	"github.com/theRiddler9/OrderFlow/internal/events"
	"github.com/theRiddler9/OrderFlow/internal/workflow"
)

func paymentRequestedEnvelope(payload string) events.Envelope {
	return events.Envelope{
		ID:          uuid.NewString(),
		Type:        "payment.requested.v1",
		AggregateID: uuid.NewString(),
		Payload:     []byte(payload),
	}
}

func validPaymentPayload(scenario string) string {
	return `{
		"payment_id":"` + uuid.NewString() + `",
		"reservation_id":"` + uuid.NewString() + `",
		"event_id":"` + uuid.NewString() + `",
		"customer_id":"` + uuid.NewString() + `",
		"quantity":2,
		"scenario":"` + scenario + `"
	}`
}

func TestParsePaymentRequestedAcceptsBothScenarios(t *testing.T) {
	for _, scenario := range []string{"approve", "decline"} {
		payload, err := workflow.ParsePaymentRequested(paymentRequestedEnvelope(validPaymentPayload(scenario)))
		if err != nil {
			t.Fatalf("ParsePaymentRequested(%s) error = %v", scenario, err)
		}
		if payload.Scenario != scenario || payload.Quantity != 2 {
			t.Fatalf("payload = %+v", payload)
		}
	}
}

func TestParsePaymentRequestedRejectsInvalidPayloadsAsPoison(t *testing.T) {
	tests := map[string]string{
		"unknown scenario":   `{"payment_id":"` + uuid.NewString() + `","reservation_id":"` + uuid.NewString() + `","scenario":"4242424242424242"}`,
		"missing scenario":   `{"payment_id":"` + uuid.NewString() + `","reservation_id":"` + uuid.NewString() + `"}`,
		"bad payment id":     `{"payment_id":"nope","reservation_id":"` + uuid.NewString() + `","scenario":"approve"}`,
		"bad reservation id": `{"payment_id":"` + uuid.NewString() + `","reservation_id":"nope","scenario":"approve"}`,
	}

	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := workflow.ParsePaymentRequested(paymentRequestedEnvelope(payload))
			if err == nil {
				t.Fatal("ParsePaymentRequested() error = nil, want validation error")
			}
			// Retrying cannot fix a malformed payload, so it must skip the retry loop.
			if !errors.Is(err, workflow.ErrPoisonMessage) {
				t.Fatalf("error = %v, want it to wrap ErrPoisonMessage", err)
			}
			if !workflow.IsPoisonError(err) {
				t.Fatalf("IsPoisonError(%v) = false, want true", err)
			}
		})
	}
}

func TestParsePaymentRequestedRejectsWrongEventType(t *testing.T) {
	event := paymentRequestedEnvelope(validPaymentPayload("approve"))
	event.Type = "reservation.held.v1"

	if _, err := workflow.ParsePaymentRequested(event); !errors.Is(err, workflow.ErrPoisonMessage) {
		t.Fatalf("error = %v, want ErrPoisonMessage", err)
	}
}

func TestParsePaymentRequestedMalformedJSONIsPoison(t *testing.T) {
	_, err := workflow.ParsePaymentRequested(paymentRequestedEnvelope(`{"payment_id":`))
	if !workflow.IsPoisonError(err) {
		t.Fatalf("IsPoisonError(%v) = false, want true for malformed JSON", err)
	}
}

func TestProcessorAcknowledgesPaymentOutcomeEventsWithoutDatabaseWork(t *testing.T) {
	// The outcome events share the topic with the saga events. They are notifications for
	// other consumers, so the worker must acknowledge them instead of failing with
	// "unsupported event type" and routing them to the dead-letter queue.
	processor := workflow.NewProcessor(nil, "approve").WithPayments(config.PaymentModeCheckout, 0)

	for _, eventType := range []string{"payment.succeeded.v1", "payment.declined.v1"} {
		if err := processor.Handle(context.Background(), events.Envelope{Type: eventType}); err != nil {
			t.Fatalf("Handle(%s) error = %v, want nil", eventType, err)
		}
	}
}

func TestProcessorLeavesHeldReservationsAloneInCheckoutMode(t *testing.T) {
	// In checkout mode the worker must not settle a hold on its own. A nil database proves
	// it returns before touching PostgreSQL: any state change would fail here.
	processor := workflow.NewProcessor(nil, "approve").WithPayments(config.PaymentModeCheckout, 0)

	err := processor.Handle(context.Background(), events.Envelope{
		ID:          uuid.NewString(),
		Type:        "reservation.held.v1",
		AggregateID: uuid.NewString(),
		Payload:     []byte(`{"reservation_id":"` + uuid.NewString() + `"}`),
	})
	if err != nil {
		t.Fatalf("Handle(reservation.held.v1) error = %v, want nil in checkout mode", err)
	}
}

func TestProcessorStillSettlesHeldReservationsInAutoMode(t *testing.T) {
	// Auto mode keeps the original behaviour, which needs the database.
	processor := workflow.NewProcessor(nil, "approve")

	err := processor.Handle(context.Background(), events.Envelope{
		ID:          uuid.NewString(),
		Type:        "reservation.held.v1",
		AggregateID: uuid.NewString(),
		Payload:     []byte(`{"reservation_id":"` + uuid.NewString() + `"}`),
	})
	if err == nil {
		t.Fatal("Handle(reservation.held.v1) error = nil, want the workflow database requirement in auto mode")
	}
}

func TestProcessorIgnoresPaymentRequestsInAutoMode(t *testing.T) {
	processor := workflow.NewProcessor(nil, "approve")

	err := processor.Handle(context.Background(), paymentRequestedEnvelope(validPaymentPayload("approve")))
	if err != nil {
		t.Fatalf("Handle(payment.requested.v1) error = %v, want nil (acknowledged) in auto mode", err)
	}
}

func TestProcessorRequiresDatabaseForPaymentRequestsInCheckoutMode(t *testing.T) {
	processor := workflow.NewProcessor(nil, "approve").WithPayments(config.PaymentModeCheckout, 0)

	err := processor.Handle(context.Background(), paymentRequestedEnvelope(validPaymentPayload("approve")))
	if err == nil {
		t.Fatal("Handle(payment.requested.v1) error = nil, want database requirement error")
	}
	if workflow.IsPoisonError(err) {
		t.Fatalf("a missing database is a retryable fault, not poison: %v", err)
	}
}

func TestProcessorRejectsUnknownEventTypes(t *testing.T) {
	processor := workflow.NewProcessor(nil, "approve").WithPayments(config.PaymentModeCheckout, 0)

	err := processor.Handle(context.Background(), events.Envelope{Type: "payment.refunded.v1"})
	if err == nil {
		t.Fatal("Handle() error = nil, want unsupported event type")
	}
}
