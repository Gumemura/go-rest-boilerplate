package auth

import (
	"github.com/go-rest-boilerplate/internal/business/usecases/auth"
)

type Handler struct {
	usecase auth.Usecase
}

func NewHandler(usecase auth.Usecase) Handler {
	return Handler{usecase: usecase}
}
