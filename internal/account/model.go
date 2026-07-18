package account

import "time"

type Profile struct {
	UID       string    `json:"uid,omitempty"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	ImageURL  string    `json:"imageUrl"`
	Role      string    `json:"role"`
	Plan      string    `json:"plan,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

// UpdateProfileInput keeps role for wire compatibility with the 2023 client.
// Service code deliberately ignores it: account privilege is never user-writable.
type UpdateProfileInput struct {
	Name     string `json:"name"`
	ImageURL string `json:"imageUrl,omitempty"`
	Role     string `json:"role,omitempty"`
}
