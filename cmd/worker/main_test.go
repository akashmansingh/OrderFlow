package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/theRiddler9/OrderFlow/internal/events"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestHandleRecordCommitsAfterSuccessfulHandling(t *testing.T) {
	committer := &recordCommitter{}
	handler := handlerFunc(func(context.Context, events.Envelope) error { return nil })

	if err := handleRecord(context.Background(), handler, committer, heldRecord(t)); err != nil {
		t.Fatalf("handleRecord() error = %v", err)
	}
	if committer.calls != 1 {
		t.Fatalf("CommitRecords calls = %d, want 1", committer.calls)
	}
}

func TestHandleRecordDoesNotCommitAfterWorkflowFailure(t *testing.T) {
	committer := &recordCommitter{}
	handler := handlerFunc(func(context.Context, events.Envelope) error { return errors.New("database unavailable") })

	if err := handleRecord(context.Background(), handler, committer, heldRecord(t)); err == nil {
		t.Fatal("handleRecord() error = nil, want handler error")
	}
	if committer.calls != 0 {
		t.Fatalf("CommitRecords calls = %d, want 0", committer.calls)
	}
}

type handlerFunc func(context.Context, events.Envelope) error

func (handler handlerFunc) Handle(ctx context.Context, event events.Envelope) error {
	return handler(ctx, event)
}

type recordCommitter struct{ calls int }

func (committer *recordCommitter) CommitRecords(context.Context, ...*kgo.Record) error {
	committer.calls++
	return nil
}

func heldRecord(t *testing.T) *kgo.Record {
	t.Helper()
	value, err := json.Marshal(events.Envelope{
		ID:          "d2719c30-1edf-4c5f-9273-cb1f55a3797e",
		Type:        "reservation.held.v1",
		AggregateID: "96e74b3e-01aa-469e-802d-f04e1dcfbdff",
		OccurredAt:  time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC),
		Payload:     []byte(`{"reservation_id":"61c26b50-c02a-4a86-a46d-1f2ea1078b9f","event_id":"96e74b3e-01aa-469e-802d-f04e1dcfbdff","customer_id":"8a9db6db-01eb-4fd3-aeba-dd1255a4f970","quantity":1,"expires_at":"2026-11-17T12:10:00Z"}`),
	})
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	return &kgo.Record{Value: value}
}
