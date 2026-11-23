package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/theRiddler9/OrderFlow/internal/events"
	"github.com/theRiddler9/OrderFlow/internal/metrics"
	"github.com/theRiddler9/OrderFlow/internal/workflow"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestHandleRecordCommitsAfterSuccessfulHandling(t *testing.T) {
	committer := &recordCommitter{}
	dlq := &stubDLQPublisher{}
	handler := handlerFunc(func(context.Context, events.Envelope) error { return nil })

	if err := handleRecord(context.Background(), handler, committer, dlq, nil, nil, heldRecord(t)); err != nil {
		t.Fatalf("handleRecord() error = %v", err)
	}
	if committer.calls != 1 {
		t.Fatalf("CommitRecords calls = %d, want 1", committer.calls)
	}
	if len(dlq.published) != 0 {
		t.Fatalf("DLQ calls = %d, want 0", len(dlq.published))
	}
}

func TestHandleRecordRoutesPoisonRecordToDLQAndCommits(t *testing.T) {
	committer := &recordCommitter{}
	dlq := &stubDLQPublisher{}
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)
	handler := handlerFunc(func(context.Context, events.Envelope) error { return nil })

	poisonRecord := &kgo.Record{Value: []byte("this is not valid json or envelope")}

	if err := handleRecord(context.Background(), handler, committer, dlq, m, nil, poisonRecord); err != nil {
		t.Fatalf("handleRecord() error = %v", err)
	}
	if committer.calls != 1 {
		t.Fatalf("expected commit to unblock partition, got calls = %d", committer.calls)
	}
	if len(dlq.published) != 1 {
		t.Fatalf("expected 1 DLQ publish call, got %d", len(dlq.published))
	}
}

func TestHandleRecordRetriesAndRoutesToDLQOnExhaustion(t *testing.T) {
	committer := &recordCommitter{}
	dlq := &stubDLQPublisher{}
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)

	attempts := 0
	handler := handlerFunc(func(context.Context, events.Envelope) error {
		attempts++
		return errors.New("transient database connection drop")
	})

	if err := handleRecord(context.Background(), handler, committer, dlq, m, nil, heldRecord(t)); err != nil {
		t.Fatalf("handleRecord() error = %v", err)
	}

	// 1 initial attempt + 3 retries = 4 attempts
	if attempts != 4 {
		t.Fatalf("attempts = %d, want 4", attempts)
	}
	if committer.calls != 1 {
		t.Fatalf("CommitRecords calls = %d, want 1 (to advance partition after DLQ)", committer.calls)
	}
	if len(dlq.published) != 1 {
		t.Fatalf("DLQ calls = %d, want 1", len(dlq.published))
	}
}

func TestHandleRecordRoutesPoisonDomainErrorToDLQ(t *testing.T) {
	committer := &recordCommitter{}
	dlq := &stubDLQPublisher{}
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)

	attempts := 0
	handler := handlerFunc(func(context.Context, events.Envelope) error {
		attempts++
		return workflow.ErrPoisonMessage
	})

	if err := handleRecord(context.Background(), handler, committer, dlq, m, nil, heldRecord(t)); err != nil {
		t.Fatalf("handleRecord() error = %v", err)
	}

	// Poison error should NOT be retried
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (poison errors should not retry)", attempts)
	}
	if committer.calls != 1 {
		t.Fatalf("CommitRecords calls = %d, want 1", committer.calls)
	}
	if len(dlq.published) != 1 {
		t.Fatalf("DLQ calls = %d, want 1", len(dlq.published))
	}
}

type stubDLQPublisher struct {
	published [][]byte
}

func (s *stubDLQPublisher) PublishDLQ(ctx context.Context, originalRecord []byte, reason string, retryCount int) error {
	s.published = append(s.published, originalRecord)
	return nil
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
