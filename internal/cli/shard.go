package cli

import (
	"errors"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
)

func parseShard(spec string) (k, n int, err error) {
	return 0, 0, errors.New("not implemented")
}

func shardRules(rls []*config.Rule, prior engine.Timing, k, n int) []*config.Rule {
	return nil
}
