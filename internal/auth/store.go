package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const uniqueViolationCode = "23505"

var (
	ErrDuplicateEmail = errors.New("email already registered")
	ErrUserNotFound   = errors.New("user not found")
)

// User is the public representation of a registered account.
type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// Store persists users. Password hashes never leave the auth package through public JSON.
type Store interface {
	CreateUser(ctx context.Context, email, passwordHash string) (User, error)
	FindUserByEmail(ctx context.Context, email string) (User, string, error)
}

// PostgresStore is the authoritative user store.
type PostgresStore struct {
	database *pgxpool.Pool
}

func NewPostgresStore(database *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{database: database}
}

func (store *PostgresStore) CreateUser(ctx context.Context, email, passwordHash string) (User, error) {
	const query = `
		INSERT INTO users (id, email, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id, email, created_at`

	var user User
	err := store.database.QueryRow(ctx, query, uuid.New(), email, passwordHash).
		Scan(&user.ID, &user.Email, &user.CreatedAt)
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && pgError.Code == uniqueViolationCode {
			return User{}, ErrDuplicateEmail
		}
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

func (store *PostgresStore) FindUserByEmail(ctx context.Context, email string) (User, string, error) {
	const query = `
		SELECT id, email, password_hash, created_at
		FROM users
		WHERE email = $1`

	var user User
	var passwordHash string
	err := store.database.QueryRow(ctx, query, email).
		Scan(&user.ID, &user.Email, &passwordHash, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", ErrUserNotFound
	}
	if err != nil {
		return User{}, "", fmt.Errorf("find user by email: %w", err)
	}
	return user, passwordHash, nil
}
