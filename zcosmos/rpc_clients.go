package zcosmos

import (
	"context"
	"strings"

	"github.com/hashicorp/go-multierror"
	tm_rpc_client_http "github.com/tendermint/tendermint/rpc/client/http"
)

type rpcClients struct {
	clients []*tm_rpc_client_http.HTTP
}

func (cs *rpcClients) do(ctx context.Context, f func(*tm_rpc_client_http.HTTP) error) error {
	merr := &multierror.Error{ErrorFormat: func(errs []error) string {
		strs := make([]string, len(errs))
		for i := range errs {
			strs[i] = errs[i].Error()
		}
		return strings.Join(strs, "; ")
	}}

	for _, c := range cs.clients {
		err := f(c)
		// TODO: metrics
		switch {
		case err == nil:
			return nil
		case err != nil:
			merr = multierror.Append(merr, err)
		}
	}

	return merr.ErrorOrNil()
}
