package handler

import "github.com/Wei-Shaw/sub2api/internal/service"

// IQTestModels exposes the existing account-scoped model discovery service.
func (h *OpenAIGatewayHandler) IQTestModels() *service.OpenAIGatewayService {
	if h == nil {
		return nil
	}
	return h.gatewayService
}
