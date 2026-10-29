package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

type UserRepo struct{ pool *pgxpool.Pool }

func NewUserRepo(pool *pgxpool.Pool) *UserRepo { return &UserRepo{pool: pool} }

func (r *UserRepo) Create(ctx context.Context, u *domain.User) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash, role, display_name, email, sso_sub)
		 VALUES ($1,$2,$3,$4,$5,NULLIF($6,'')) RETURNING id`,
		u.Username, u.PasswordHash, u.Role, u.DisplayName, u.Email, u.SSOSub).Scan(&u.ID)
}

const userSelectCols = `id, username, password_hash, role, COALESCE(system_role,'user'), COALESCE(email,''), COALESCE(sso_sub,''), display_name, is_active, created_at, updated_at`

func scanUser(row interface{ Scan(...any) error }) (*domain.User, error) {
	u := &domain.User{}
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.SystemRole, &u.Email,
		&u.SSOSub, &u.DisplayName, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

func (r *UserRepo) GetByID(ctx context.Context, id string) (*domain.User, error) {
	u, err := scanUser(r.pool.QueryRow(ctx,
		`SELECT `+userSelectCols+` FROM users WHERE id=$1`, id))
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}
	return u, nil
}

func (r *UserRepo) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	u, err := scanUser(r.pool.QueryRow(ctx,
		`SELECT `+userSelectCols+` FROM users WHERE username=$1`, username))
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}
	return u, nil
}

// GetByEmail matches the email without regard to case (identity providers and
// people disagree about capitals); an exact match wins if several differ only in case.
func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	u, err := scanUser(r.pool.QueryRow(ctx,
		`SELECT `+userSelectCols+` FROM users WHERE email <> '' AND lower(email)=lower($1)
		 ORDER BY (email=$1) DESC, created_at LIMIT 1`, email))
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}
	return u, nil
}

// FindByEmailFold returns every user with the email (case-insensitive).
func (r *UserRepo) FindByEmailFold(ctx context.Context, email string) ([]*domain.User, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+userSelectCols+` FROM users WHERE email <> '' AND lower(email)=lower($1) ORDER BY created_at`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// FindByUsername is GetByUsername that reports "no such user" as nil, not an error.
func (r *UserRepo) FindByUsername(ctx context.Context, username string) (*domain.User, error) {
	u, err := scanUser(r.pool.QueryRow(ctx, `SELECT `+userSelectCols+` FROM users WHERE username=$1`, username))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

func (r *UserRepo) GetBySSOSub(ctx context.Context, sub string) (*domain.User, error) {
	u, err := scanUser(r.pool.QueryRow(ctx,
		`SELECT `+userSelectCols+` FROM users WHERE sso_sub=$1`, sub))
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}
	return u, nil
}

func (r *UserRepo) LinkSSO(ctx context.Context, userID string, sub string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE users SET sso_sub=$1, updated_at=now() WHERE id=$2`, sub, userID)
	return err
}

func (r *UserRepo) List(ctx context.Context) ([]*domain.User, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, username, role, COALESCE(system_role,'user'), COALESCE(email,''), COALESCE(sso_sub,''),
		        display_name, is_active FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []*domain.User
	for rows.Next() {
		u := &domain.User{}
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.SystemRole, &u.Email, &u.SSOSub,
			&u.DisplayName, &u.IsActive); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func (r *UserRepo) Update(ctx context.Context, u *domain.User) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE users SET role=$1, display_name=$2, password_hash=$3, updated_at=now() WHERE id=$4`,
		u.Role, u.DisplayName, u.PasswordHash, u.ID)
	return err
}

func (r *UserRepo) UpdateFields(ctx context.Context, id string, fields map[string]interface{}) error {
	sets := []string{}
	args := []interface{}{}
	idx := 1
	for _, k := range []string{"role", "system_role", "display_name", "email", "sso_sub", "is_active", "password_hash"} {
		if v, ok := fields[k]; ok {
			sets = append(sets, fmt.Sprintf("%s=$%d", k, idx))
			args = append(args, v)
			idx++
		}
	}
	if len(sets) == 0 {
		return nil
	}
	q := fmt.Sprintf("UPDATE users SET %s, updated_at=now() WHERE id=$%d",
		strings.Join(sets, ", "), idx)
	args = append(args, id)
	_, err := r.pool.Exec(ctx, q, args...)
	return err
}

func (r *UserRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, id)
	return err
}
