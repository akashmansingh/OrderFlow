package reservation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"
)

var (
	ErrEventNotFound         = errors.New("event not found")
	ErrReservationNotFound   = errors.New("reservation not found")
	ErrInsufficientInventory = errors.New("insufficient inventory")
	ErrIdempotencyConflict   = errors.New("idempotency key was used with a different request")
)

type Event struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	StartsAt          time.Time `json:"starts_at"`
	Capacity          int       `json:"capacity"`
	HeldQuantity      int       `json:"held_quantity"`
	ConfirmedQuantity int       `json:"confirmed_quantity"`
}

type Reservation struct {
	ID         string     `json:"id"`
	EventID    string     `json:"event_id"`
	CustomerID string     `json:"customer_id"`
	Quantity   int        `json:"quantity"`
	Status     string     `json:"status"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type CreateEventInput struct {
	Name     string
	StartsAt time.Time
	Capacity int
}

type CreateReservationInput struct {
	EventID        string
	CustomerID     string
	Quantity       int
	IdempotencyKey string
	RequestHash    []byte
}

type inventoryCache interface {
	IsSoldOut(ctx context.Context, eventID string) (bool, error)
	MarkSoldOut(ctx context.Context, eventID string, ttl time.Duration) error
}

type Service struct {
	database     *pgxpool.Pool
	holdDuration time.Duration
	cache        inventoryCache
	soldOutTTL   time.Duration
	sfGroup      singleflight.Group
}

func NewService(database *pgxpool.Pool, holdDuration time.Duration) *Service {
	return &Service{database: database, holdDuration: holdDuration}
}

func (service *Service) WithCache(cache inventoryCache, ttl time.Duration) *Service {
	service.cache = cache
	service.soldOutTTL = ttl
	return service
}

func (service *Service) isSoldOut(ctx context.Context, eventID string) bool {
	if service.cache == nil {
		return false
	}
	val, err, _ := service.sfGroup.Do("soldout:"+eventID, func() (any, error) {
		return service.cache.IsSoldOut(ctx, eventID)
	})
	if err != nil || val == nil {
		return false
	}
	return val.(bool)
}

func (service *Service) CreateEvent(ctx context.Context, input CreateEventInput) (Event, error) {
	if input.Name == "" || input.Capacity <= 0 || input.StartsAt.IsZero() {
		return Event{}, fmt.Errorf("name, starts_at, and positive capacity are required")
	}

	const query = `
		INSERT INTO events (id, name, starts_at, capacity)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, starts_at, capacity, held_quantity, confirmed_quantity`

	return scanEvent(service.database.QueryRow(ctx, query, uuid.New(), input.Name, input.StartsAt.UTC(), input.Capacity))
}

func (service *Service) GetEvent(ctx context.Context, eventID string) (Event, error) {
	if _, err := uuid.Parse(eventID); err != nil {
		return Event{}, ErrEventNotFound
	}

	val, err, _ := service.sfGroup.Do("event:"+eventID, func() (any, error) {
		const query = `
			SELECT id, name, starts_at, capacity, held_quantity, confirmed_quantity
			FROM events
			WHERE id = $1`

		event, err := scanEvent(service.database.QueryRow(ctx, query, eventID))
		if errors.Is(err, pgx.ErrNoRows) {
			return Event{}, ErrEventNotFound
		}
		return event, err
	})
	if err != nil {
		return Event{}, err
	}
	return val.(Event), nil
}

func (service *Service) CreateReservation(ctx context.Context, input CreateReservationInput) (Reservation, error) {
	if _, err := uuid.Parse(input.EventID); err != nil {
		return Reservation{}, ErrEventNotFound
	}
	if _, err := uuid.Parse(input.CustomerID); err != nil || input.Quantity <= 0 || input.IdempotencyKey == "" || len(input.RequestHash) == 0 {
		return Reservation{}, fmt.Errorf("customer_id, positive quantity, idempotency key, and request hash are required")
	}

	if service.isSoldOut(ctx, input.EventID) {
		return Reservation{}, ErrInsufficientInventory
	}

	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Reservation{}, fmt.Errorf("begin reservation transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	existingRequestHash, existingReservationID, err := getIdempotencyRecord(ctx, tx, input.IdempotencyKey)
	if err == nil {
		if !bytes.Equal(existingRequestHash, input.RequestHash) {
			return Reservation{}, ErrIdempotencyConflict
		}
		return getReservation(ctx, tx, existingReservationID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, fmt.Errorf("read idempotency key: %w", err)
	}

	if err := reserveInventory(ctx, tx, input.EventID, input.Quantity); err != nil {
		if errors.Is(err, ErrInsufficientInventory) && service.cache != nil && service.soldOutTTL > 0 {
			_ = service.cache.MarkSoldOut(ctx, input.EventID, service.soldOutTTL)
		}
		return Reservation{}, err
	}

	now := time.Now().UTC()
	expiresAt := now.Add(service.holdDuration)
	reservationID := uuid.New()

	const insertReservation = `
		INSERT INTO reservations (id, event_id, customer_id, quantity, status, expires_at)
		VALUES ($1, $2, $3, $4, 'held', $5)
		RETURNING id, event_id, customer_id, quantity, status, expires_at, created_at`
	reservation, err := scanReservation(tx.QueryRow(ctx, insertReservation, reservationID, input.EventID, input.CustomerID, input.Quantity, expiresAt))
	if err != nil {
		return Reservation{}, fmt.Errorf("create reservation: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO idempotency_keys (key, request_hash, reservation_id) VALUES ($1, $2, $3)`,
		input.IdempotencyKey,
		input.RequestHash,
		reservationID,
	); err != nil {
		return Reservation{}, fmt.Errorf("save idempotency key: %w", err)
	}

	payload, err := json.Marshal(map[string]any{
		"reservation_id": reservation.ID,
		"event_id":       reservation.EventID,
		"customer_id":    reservation.CustomerID,
		"quantity":       reservation.Quantity,
		"expires_at":     reservation.ExpiresAt,
	})
	if err != nil {
		return Reservation{}, fmt.Errorf("encode reservation event: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO outbox_events (id, aggregate_id, event_type, payload) VALUES ($1, $2, $3, $4)`,
		uuid.New(),
		reservationID,
		"reservation.held.v1",
		payload,
	); err != nil {
		return Reservation{}, fmt.Errorf("write reservation outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Reservation{}, fmt.Errorf("commit reservation transaction: %w", err)
	}

	return reservation, nil
}

func (service *Service) GetReservation(ctx context.Context, reservationID string) (Reservation, error) {
	if _, err := uuid.Parse(reservationID); err != nil {
		return Reservation{}, ErrReservationNotFound
	}

	reservation, err := getReservation(ctx, service.database, reservationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, ErrReservationNotFound
	}
	return reservation, err
}

// GetReservationForCustomer prevents one authenticated user from reading another user's reservation.
// A forbidden reservation is reported as not found so ownership cannot be probed.
func (service *Service) GetReservationForCustomer(ctx context.Context, reservationID, customerID string) (Reservation, error) {
	if _, err := uuid.Parse(customerID); err != nil {
		return Reservation{}, ErrReservationNotFound
	}

	reservation, err := service.GetReservation(ctx, reservationID)
	if err != nil {
		return Reservation{}, err
	}
	if reservation.CustomerID != customerID {
		return Reservation{}, ErrReservationNotFound
	}
	return reservation, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func getIdempotencyRecord(ctx context.Context, tx pgx.Tx, key string) ([]byte, string, error) {
	var requestHash []byte
	var reservationID string
	err := tx.QueryRow(ctx,
		`SELECT request_hash, reservation_id FROM idempotency_keys WHERE key = $1`,
		key,
	).Scan(&requestHash, &reservationID)
	return requestHash, reservationID, err
}

func reserveInventory(ctx context.Context, tx pgx.Tx, eventID string, quantity int) error {
	var updatedEventID string
	err := tx.QueryRow(ctx, `
		UPDATE events
		SET held_quantity = held_quantity + $2, updated_at = NOW()
		WHERE id = $1 AND capacity - held_quantity - confirmed_quantity >= $2
		RETURNING id`, eventID, quantity).Scan(&updatedEventID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("reserve inventory: %w", err)
	}

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM events WHERE id = $1)`, eventID).Scan(&exists); err != nil {
		return fmt.Errorf("check event existence: %w", err)
	}
	if !exists {
		return ErrEventNotFound
	}
	return ErrInsufficientInventory
}

func getReservation(ctx context.Context, querier rowQuerier, reservationID string) (Reservation, error) {
	const query = `
		SELECT id, event_id, customer_id, quantity, status, expires_at, created_at
		FROM reservations
		WHERE id = $1`
	return scanReservation(querier.QueryRow(ctx, query, reservationID))
}

func scanEvent(row rowScanner) (Event, error) {
	var event Event
	err := row.Scan(
		&event.ID,
		&event.Name,
		&event.StartsAt,
		&event.Capacity,
		&event.HeldQuantity,
		&event.ConfirmedQuantity,
	)
	return event, err
}

func scanReservation(row rowScanner) (Reservation, error) {
	var reservation Reservation
	err := row.Scan(
		&reservation.ID,
		&reservation.EventID,
		&reservation.CustomerID,
		&reservation.Quantity,
		&reservation.Status,
		&reservation.ExpiresAt,
		&reservation.CreatedAt,
	)
	return reservation, err
}
