package automation

import (
	"context"

	"github.com/zaneway/theone/internal/retention"
)

func (s *Service) RunRetention(ctx context.Context, req retention.RunRequest) (retention.RunResponse, error) {
	return retention.NewService(s.cfg, s.repo).Run(ctx, req)
}
