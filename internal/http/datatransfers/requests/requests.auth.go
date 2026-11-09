package requests

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}