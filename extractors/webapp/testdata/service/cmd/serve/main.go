package main

import (
	"github.com/Khan/webapp/pkg/web/gqlclient"
	"github.com/Khan/webapp/pkg/web/serve"
)

func main() {
	ctx := kacontext.NewForProd()
	_ = gqlclient.NewClient(ctx, ctx.Secrets())
	_ = gqlclient.NewSender().Send(ctx)
	serve.Main(ctx, serve.HandleGraphQL(schema))
}
